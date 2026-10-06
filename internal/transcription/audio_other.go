//go:build !darwin || !cgo

package transcription

import "errors"

func startSystemAudio() (audioCapture, error) {
	return nil, errors.New("system audio capture requires macOS 13 or newer and a build with cgo enabled")
}
