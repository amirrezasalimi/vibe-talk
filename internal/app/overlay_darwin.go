//go:build darwin && cgo

package app

import "vibe-talk/internal/transcription"

// handle is MyGo's borrowed NSWindow pointer; native code does not retain it.
func configureCaptionOverlay(handle uintptr) {
	transcription.ConfigureCaptionOverlay(handle)
}
