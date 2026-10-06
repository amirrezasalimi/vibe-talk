#ifndef VIBE_TALK_AUDIO_NATIVE_H
#define VIBE_TALK_AUDIO_NATIVE_H

#include <stddef.h>

// Opaque retained Objective-C capture. No callbacks cross into Go.
typedef void *vt_audio_capture;
vt_audio_capture vt_audio_start(char *error, size_t error_capacity);
// Positive: samples copied; zero: drained EOF; negative: drained failure.
ptrdiff_t vt_audio_read(vt_audio_capture capture, float *samples, size_t capacity,
                        char *error, size_t error_capacity);
// Stops and waits for shutdown, but preserves buffered samples for subsequent reads.
int vt_audio_close(vt_audio_capture capture, char *error, size_t error_capacity);
// Releases the handle; asynchronous native work retains its own lifetime.
void vt_audio_release(vt_audio_capture capture);

#endif
