//go:build !darwin || !cgo

package app

// Other platforms keep MyGo's portable always-on-top and click-through behavior.
func configureCaptionOverlay(_ uintptr) {}
