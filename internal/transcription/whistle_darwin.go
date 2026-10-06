//go:build darwin && arm64

package transcription

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

const whistleOutputCapacity = 262144

// The caller serializes whole sessions, including initialization and Flush:
// Needle has process-global, non-thread-safe streaming state.
type whistleEngine struct {
	handle    uintptr
	model     []byte // mmap-backed: native code may retain this address indefinitely.
	load      func(uintptr, uint64) int32
	lastError func() uintptr
	process   func(uintptr, int32, uintptr, uintptr, uintptr, int32) int32
	stop      func(uintptr, int32) int32
}

var whistleState struct {
	mu     sync.Mutex
	once   sync.Once
	engine *whistleEngine
}

func newSpeechEngine() (speechEngine, error) {
	whistleState.mu.Lock()
	defer whistleState.mu.Unlock()
	if whistleState.engine != nil {
		return whistleState.engine, nil
	}
	var initErr error
	whistleState.once.Do(func() {
		whistleState.engine, initErr = loadWhistleEngine()
	})
	if initErr != nil {
		// A missing or invalid runtime can be repaired without restarting the app.
		whistleState.once = sync.Once{}
		return nil, initErr
	}
	return whistleState.engine, nil
}

func whistleRuntimeDirectory() (string, error) {
	check := func(dir string) (string, error) {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		for _, name := range []string{"libwhistle.dylib", "whistle.cact"} {
			info, err := os.Stat(filepath.Join(absolute, name))
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				return "", fmt.Errorf("%s is not a nonempty regular file", filepath.Join(absolute, name))
			}
		}
		return absolute, nil
	}
	if override := os.Getenv("VIBE_TALK_RUNTIME"); override != "" {
		dir, err := check(override)
		if err != nil {
			return "", fmt.Errorf("VIBE_TALK_RUNTIME: %w", err)
		}
		return dir, nil
	}
	var candidates []string
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates, filepath.Join(base, "runtime"), filepath.Join(base, "..", "Resources", "runtime"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "runtime"), filepath.Join(cwd, "resources", "runtime"))
	}
	for _, candidate := range candidates {
		if dir, err := check(candidate); err == nil {
			return dir, nil
		}
	}
	return "", errors.New("Whistle runtime not found; run scripts/setup-transcription.sh or set VIBE_TALK_RUNTIME (libwhistle.dylib and whistle.cact required)")
}

func loadWhistleEngine() (*whistleEngine, error) {
	dir, err := whistleRuntimeDirectory()
	if err != nil {
		return nil, err
	}
	handle, err := purego.Dlopen(filepath.Join(dir, "libwhistle.dylib"), purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load Whistle library: %w", err)
	}
	e := &whistleEngine{handle: handle}
	success := false
	defer func() {
		if !success {
			// Do not unmap a model passed to needle_load: even a failed load may
			// retain its address. Successful mappings and handles also live forever.
			_ = purego.Dlclose(handle)
		}
	}()
	for _, symbol := range []struct {
		name string
		fn   any
	}{
		{"needle_load", &e.load},
		{"needle_last_error", &e.lastError},
		{"needle_stream_transcribe_process", &e.process},
		{"needle_stream_transcribe_stop", &e.stop},
	} {
		address, err := purego.Dlsym(handle, symbol.name)
		if err != nil {
			return nil, fmt.Errorf("Whistle symbol %s: %w", symbol.name, err)
		}
		purego.RegisterFunc(symbol.fn, address)
	}
	file, err := os.Open(filepath.Join(dir, "whistle.cact"))
	if err != nil {
		return nil, fmt.Errorf("open Whistle model: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > int64(int(^uint(0)>>1)) {
		return nil, errors.New("invalid Whistle model size")
	}
	e.model, err = syscall.Mmap(int(file.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("map Whistle model: %w", err)
	}
	result := e.load(uintptr(unsafe.Pointer(&e.model[0])), uint64(len(e.model)))
	runtime.KeepAlive(e.model)
	if result < 0 {
		return nil, e.nativeError("load Whistle model")
	}
	success = true
	return e, nil
}

func (e *whistleEngine) nativeError(operation string) error {
	pointer := e.lastError()
	if pointer == 0 {
		return fmt.Errorf("%s: native engine failed without an error message", operation)
	}
	// The runtime owns this NUL-terminated string until the next API call.
	var message []byte
	for offset := uintptr(0); offset < 65536; offset++ {
		b := *(*byte)(unsafe.Pointer(pointer + offset))
		if b == 0 {
			break
		}
		message = append(message, b)
	}
	return fmt.Errorf("%s: %s", operation, message)
}

func (e *whistleEngine) Process(samples []float32, language string) (Event, error) {
	if len(samples) == 0 || len(samples) > 30*16000 {
		return Event{}, errors.New("Whistle requires 1–480000 mono 16 kHz samples per call")
	}
	language, err := NormalizeLanguage(language)
	if err != nil {
		return Event{}, err
	}
	if strings.IndexByte(language, 0) >= 0 {
		return Event{}, errors.New("Whistle language contains NUL")
	}
	var languageBytes []byte
	var languagePointer uintptr
	if language != "" {
		languageBytes = append([]byte(language), 0)
		languagePointer = uintptr(unsafe.Pointer(&languageBytes[0]))
	}
	out := make([]byte, whistleOutputCapacity)
	result := e.process(uintptr(unsafe.Pointer(&samples[0])), int32(len(samples)), languagePointer, 0,
		uintptr(unsafe.Pointer(&out[0])), int32(len(out)))
	runtime.KeepAlive(samples)
	runtime.KeepAlive(languageBytes)
	runtime.KeepAlive(out)
	if result < 0 {
		return Event{}, e.nativeError("Whistle process")
	}
	return decodeWhistleEvent(out)
}

func (e *whistleEngine) Flush() (Event, error) {
	out := make([]byte, whistleOutputCapacity)
	result := e.stop(uintptr(unsafe.Pointer(&out[0])), int32(len(out)))
	runtime.KeepAlive(out)
	if result < 0 {
		return Event{}, e.nativeError("Whistle flush")
	}
	return decodeWhistleEvent(out)
}

func decodeWhistleEvent(out []byte) (Event, error) {
	end := bytes.IndexByte(out, 0)
	if end < 0 {
		return Event{}, errors.New("Whistle output exceeds JSON buffer capacity")
	}
	var event Event
	if err := json.Unmarshal(out[:end], &event); err != nil {
		return Event{}, fmt.Errorf("invalid Whistle JSON: %w", err)
	}
	event.Type = "transcript"
	return event, nil
}
