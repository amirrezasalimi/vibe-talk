//go:build darwin && cgo

package transcription

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation -framework AVFoundation -framework CoreMedia -framework CoreGraphics -framework ScreenCaptureKit
#include "audio_native.h"
#include <stdint.h>
void vt_configure_caption_overlay(uintptr_t handle);
*/
import "C"

import (
	"errors"
	"io"
	"runtime"
	"sync"
	"unsafe"
)

// ConfigureCaptionOverlay configures a borrowed NSWindow on the main thread.
// Keeping native bridges in one cgo package avoids duplicate Objective-C linkage.
func ConfigureCaptionOverlay(handle uintptr) {
	C.vt_configure_caption_overlay(C.uintptr_t(handle))
}

// The handle remains owned until collection, not Close: readers must be able to
// drain final PCM after Close, including a Read already blocked in native code.
type nativeAudioCapture struct {
	handle    C.vt_audio_capture
	closeOnce sync.Once
	closeErr  error
}

// Call from a worker goroutine while the application's main event loop is free.
func startSystemAudio() (audioCapture, error) {
	return startAudio(false)
}

func startAudio(includeMicrophone bool) (audioCapture, error) {
	var microphone C.int
	if includeMicrophone {
		microphone = 1
	}
	var message [2048]C.char
	handle := C.vt_audio_start(microphone, &message[0], C.size_t(len(message)))
	if handle == nil {
		return nil, errors.New(C.GoString(&message[0]))
	}
	capture := &nativeAudioCapture{handle: handle}
	runtime.SetFinalizer(capture, func(c *nativeAudioCapture) {
		C.vt_audio_release(c.handle)
	})
	return capture, nil
}

func (c *nativeAudioCapture) Read(samples []float32) (int, error) {
	if len(samples) == 0 {
		return 0, nil
	}
	var message [2048]C.char
	n := C.vt_audio_read(c.handle, (*C.float)(unsafe.Pointer(&samples[0])),
		C.size_t(len(samples)), &message[0], C.size_t(len(message)))
	runtime.KeepAlive(c)
	if n < 0 {
		return 0, errors.New(C.GoString(&message[0]))
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func (c *nativeAudioCapture) Close() error {
	c.closeOnce.Do(func() {
		var message [2048]C.char
		if C.vt_audio_close(c.handle, &message[0], C.size_t(len(message))) != 0 {
			c.closeErr = errors.New(C.GoString(&message[0]))
		}
	})
	runtime.KeepAlive(c)
	return c.closeErr
}
