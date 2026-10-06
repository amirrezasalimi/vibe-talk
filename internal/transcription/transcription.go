package transcription

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type Event struct {
	Type     string          `json:"type"`
	Message  string          `json:"message"`
	Text     string          `json:"text"`
	Pending  string          `json:"pending"`
	Language string          `json:"language"`
	PassMS   float64         `json:"pass_ms"`
	Received json.RawMessage `json:"received"`
}

func NormalizeLanguage(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "auto":
		return "", nil
	case "en", "de", "fr", "es", "it", "nl", "pl":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported language %q; use blank, auto, en, de, fr, es, it, nl or pl", value)
	}
}

type audioCapture interface {
	Read([]float32) (int, error)
	Close() error
}
type speechEngine interface {
	Process([]float32, string) (Event, error)
	Flush() (Event, error)
}

// Needle's streaming state is process-global. Hold this across entire sessions.
var speechSessionMutex sync.Mutex

type nativeSession struct {
	mu           sync.Mutex
	stopped      bool
	capture      audioCapture
	chunkSamples int
	noiseFloorDB float64
}

func (s *nativeSession) stop() {
	s.mu.Lock()
	s.stopped = true
	capture := s.capture
	s.mu.Unlock()
	if capture != nil {
		_ = capture.Close()
	}
}
func (s *nativeSession) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

type Worker struct {
	mu                sync.Mutex
	lowCPU            bool
	includeMicrophone bool
	noiseFloorDB      float64
	active            *nativeSession
	engineFactory     func() (speechEngine, error)
	captureFactory    func() (audioCapture, error)
}

// SetInputOptions applies to the next session. Zero disables the noise gate.
func (w *Worker) SetInputOptions(microphone bool, noiseFloorDB float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.includeMicrophone = microphone
	w.noiseFloorDB = noiseFloorDB
}

// SetLowCPU applies to the next listening session. Larger chunks trade
// caption latency for fewer whole-buffer transcription passes.
func (w *Worker) SetLowCPU(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lowCPU = enabled
}

func (w *Worker) Start(language string, event func(Event), finish func(error)) error {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active != nil {
		return errors.New("already listening or stopping")
	}
	session := &nativeSession{chunkSamples: 16000, noiseFloorDB: w.noiseFloorDB}
	if w.lowCPU {
		session.chunkSamples = 32000
	}
	w.active = session
	engineFactory, captureFactory := w.engineFactory, w.captureFactory
	if engineFactory == nil {
		engineFactory = newSpeechEngine
	}
	if captureFactory == nil {
		microphone := w.includeMicrophone
		captureFactory = func() (audioCapture, error) { return startAudio(microphone) }
	}
	go func() {
		err := runNativeSession(session, language, engineFactory, captureFactory, event)
		w.mu.Lock()
		w.active = nil
		w.mu.Unlock()
		finish(err)
	}()
	return nil
}
func (w *Worker) Stop() {
	w.mu.Lock()
	session := w.active
	w.mu.Unlock()
	// Native stop may wait for asynchronous ScreenCaptureKit shutdown. Never
	// block the UI/main queue which those completions can require.
	if session != nil {
		go session.stop()
	}
}
func runNativeSession(s *nativeSession, language string, engineFactory func() (speechEngine, error), captureFactory func() (audioCapture, error), event func(Event)) (result error) {
	speechSessionMutex.Lock()
	defer speechSessionMutex.Unlock()
	if s.isStopped() {
		return nil
	}
	event(Event{Type: "status", Message: "Loading local Whistle engine…"})
	engine, err := engineFactory()
	if err != nil {
		return err
	}
	if s.isStopped() {
		return nil
	}
	capture, err := captureFactory()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.capture = capture
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		_ = capture.Close()
	}
	defer capture.Close()
	// Always reset native streaming state, even on a capture/inference error.
	defer func() {
		tail, err := engine.Flush()
		if err == nil {
			event(tail)
		} else if result == nil {
			result = err
		}
		if result == nil {
			event(Event{Type: "done"})
		}
	}()
	event(Event{Type: "status", Message: "Listening to Mac system output — no microphone"})
	chunkSamples := s.chunkSamples
	if chunkSamples == 0 {
		chunkSamples = 16000
	}
	samples := make([]float32, chunkSamples)
	held := 0
	for {
		n, readErr := capture.Read(samples[held:])
		if n < 0 || n > len(samples)-held {
			return errors.New("invalid audio capture sample count")
		}
		held += n
		if held == len(samples) || (readErr != nil && held > 0) {
			gateAudio(samples[:held], s.noiseFloorDB)
			e, err := engine.Process(samples[:held], language)
			if err != nil {
				return err
			}
			event(e)
			held = 0
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}
