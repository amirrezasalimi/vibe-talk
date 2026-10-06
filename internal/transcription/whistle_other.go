//go:build !darwin || !arm64

package transcription

import "errors"

func newSpeechEngine() (speechEngine, error) {
	return nil, errors.New("native Whistle transcription is supported only on macOS arm64 (Apple Silicon)")
}
