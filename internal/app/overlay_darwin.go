//go:build darwin && cgo

package app

/*
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>
void vt_configure_caption_overlay(uintptr_t handle);
*/
import "C"

// handle is MyGo's borrowed NSWindow pointer; native code does not retain it.
func configureCaptionOverlay(handle uintptr) {
	C.vt_configure_caption_overlay(C.uintptr_t(handle))
}
