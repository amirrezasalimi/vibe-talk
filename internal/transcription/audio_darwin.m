//go:build darwin && cgo

#import "audio_native.h"
#import <Foundation/Foundation.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#include <pthread.h>
#include <string.h>
#include <stdio.h>

#define VT_RING_CAPACITY (16000 * 8)
static NSString *const VTPermissionHelp = @"Screen/system-audio capture permission is required. Open System Settings > Privacy & Security > Screen Recording (or Screen & System Audio Recording), enable this executable or its launching terminal/app, then quit and relaunch that app. No microphone permission is needed.";

static void VTErrorCopy(NSString *message, char *destination, size_t capacity) {
    if (capacity && destination) snprintf(destination, capacity, "%s", message.UTF8String ?: "");
}

API_AVAILABLE(macos(13.0))
@interface VTAudioCapture : NSObject <SCStreamOutput, SCStreamDelegate> {
@public
    pthread_mutex_t mutex;
    pthread_cond_t condition;
    float ring[VT_RING_CAPACITY];
    size_t head, count;
    BOOL ready, done;
    NSString *failure;
@private
    dispatch_queue_t queue;
    SCStream *stream;
    AVAudioConverter *converter;
    AVAudioFormat *sourceFormat, *outputFormat;
    BOOL starting, stopping, stopIssued;
}
- (void)begin;
- (void)stop;
- (void)requestStop;
@end

@implementation VTAudioCapture
- (instancetype)init {
    if ((self = [super init])) {
        pthread_mutex_init(&mutex, NULL);
        pthread_cond_init(&condition, NULL);
        queue = dispatch_queue_create("vibe-talk.system-audio", DISPATCH_QUEUE_SERIAL);
        outputFormat = [[AVAudioFormat alloc] initWithCommonFormat:AVAudioPCMFormatFloat32
            sampleRate:16000 channels:1 interleaved:NO];
    }
    return self;
}
- (void)dealloc {
    pthread_cond_destroy(&condition);
    pthread_mutex_destroy(&mutex);
}
// These methods run only on queue; the mutex protects the consumer-facing state.
- (void)recordFailure:(NSString *)message {
    pthread_mutex_lock(&mutex);
    if (!done && !failure) failure = [message copy];
    pthread_cond_broadcast(&condition);
    pthread_mutex_unlock(&mutex);
}
- (void)fail:(NSString *)message {
    [self recordFailure:message];
    [self stop];
}
- (NSString *)describeError:(NSError *)error {
    if ([error.domain isEqualToString:SCStreamErrorDomain] && error.code == -3801)
        return [NSString stringWithFormat:@"%@ %@", error.localizedDescription, VTPermissionHelp];
    return error.localizedDescription;
}
- (BOOL)emit:(AVAudioPCMBuffer *)pcm {
    size_t n = pcm.frameLength;
    float *samples = pcm.floatChannelData[0];
    pthread_mutex_lock(&mutex);
    if (n > VT_RING_CAPACITY - count) {
        pthread_mutex_unlock(&mutex);
        [self fail:@"System audio consumer stalled: the 8-second PCM queue is full. Capture stopped rather than silently dropping audio."];
        return NO;
    }
    size_t tail = (head + count) % VT_RING_CAPACITY;
    size_t first = MIN(n, VT_RING_CAPACITY - tail);
    memcpy(ring + tail, samples, first * sizeof(float));
    memcpy(ring, samples + first, (n - first) * sizeof(float));
    count += n;
    pthread_cond_broadcast(&condition);
    pthread_mutex_unlock(&mutex);
    return YES;
}
- (BOOL)convert:(AVAudioPCMBuffer *)input {
    if (!converter) return YES;
    __block BOOL supplied = NO;
    for (;;) {
        AVAudioPCMBuffer *output = [[AVAudioPCMBuffer alloc] initWithPCMFormat:outputFormat frameCapacity:4096];
        if (!output) { [self fail:@"Cannot allocate audio conversion output."]; return NO; }
        NSError *error = nil;
        AVAudioConverterOutputStatus status = [converter convertToBuffer:output error:&error
            withInputFromBlock:^AVAudioBuffer *(AVAudioPacketCount requested, AVAudioConverterInputStatus *inputStatus) {
                (void)requested;
                if (input && !supplied) {
                    supplied = YES;
                    *inputStatus = AVAudioConverterInputStatus_HaveData;
                    return input;
                }
                *inputStatus = input ? AVAudioConverterInputStatus_NoDataNow : AVAudioConverterInputStatus_EndOfStream;
                return nil;
            }];
        if (status == AVAudioConverterOutputStatus_Error) {
            [self fail:error.localizedDescription ?: @"Audio conversion failed."]; return NO;
        }
        if (output.frameLength && ![self emit:output]) return NO;
        switch (status) {
        case AVAudioConverterOutputStatus_HaveData:
            if (!output.frameLength) { [self fail:@"Audio converter made no progress."]; return NO; }
            break;
        case AVAudioConverterOutputStatus_InputRanDry:
        case AVAudioConverterOutputStatus_EndOfStream: return YES;
        default: [self fail:@"Unknown audio converter status."]; return NO;
        }
    }
}
- (void)finish {
    pthread_mutex_lock(&mutex);
    BOOL finished = done;
    BOOL clean = !failure;
    pthread_mutex_unlock(&mutex);
    if (finished) return;
    if (clean) [self convert:nil];
    pthread_mutex_lock(&mutex);
    done = YES;
    pthread_cond_broadcast(&condition);
    pthread_mutex_unlock(&mutex);
}
- (void)stopStream {
    if (stopIssued) return;
    stopIssued = YES;
    if (!stream) { [self finish]; return; }
    // Keep both stream and delegate alive until stop completes, even if a timeout
    // wakes the Go caller first. Never free a delegate still used by ScreenCaptureKit.
    [stream stopCaptureWithCompletionHandler:^(NSError *error) {
        dispatch_async(self->queue, ^{
            if (error) [self recordFailure:[self describeError:error]];
            [self finish];
            NSError *ignored = nil;
            [self->stream removeStreamOutput:self type:SCStreamOutputTypeAudio error:&ignored];
            self->stream = nil;
        });
    }];
}
- (void)requestStop {
    dispatch_async(queue, ^{ [self stop]; });
}
- (void)stop {
    if (stopping) return;
    stopping = YES;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 8 * NSEC_PER_SEC), queue, ^{
        pthread_mutex_lock(&self->mutex);
        BOOL finished = self->done;
        pthread_mutex_unlock(&self->mutex);
        if (!finished) {
            [self recordFailure:@"Timed out waiting for ScreenCaptureKit shutdown."];
            [self finish];
        }
    });
    if (!starting) [self stopStream];
}
- (void)begin {
    dispatch_async(queue, ^{
        // Do not open a permission prompt from a worker or during automated tests.
        if (!CGPreflightScreenCaptureAccess()) { [self fail:VTPermissionHelp]; return; }
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 15 * NSEC_PER_SEC), self->queue, ^{
            pthread_mutex_lock(&self->mutex);
            BOOL pending = !self->ready && !self->done;
            pthread_mutex_unlock(&self->mutex);
            if (pending) [self fail:@"Timed out starting ScreenCaptureKit system audio."];
        });
        [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES
            completionHandler:^(SCShareableContent *content, NSError *error) {
            dispatch_async(self->queue, ^{
                if (self->stopping) return;
                if (error) { [self fail:[self describeError:error]]; return; }
                SCDisplay *display = content.displays.firstObject;
                if (!display) { [self fail:@"No capturable display. Run in a logged-in macOS GUI session with an active display."]; return; }
                SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingApplications:@[] exceptingWindows:@[]];
                SCStreamConfiguration *config = [SCStreamConfiguration new];
                config.capturesAudio = YES;
                config.excludesCurrentProcessAudio = YES;
                config.sampleRate = 48000;
                config.channelCount = 2;
                config.width = 2;
                config.height = 2;
                config.minimumFrameInterval = CMTimeMake(1, 1);
                config.queueDepth = 3;
                config.showsCursor = NO;
                self->stream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:self];
                NSError *addError = nil;
                if (![self->stream addStreamOutput:self type:SCStreamOutputTypeAudio sampleHandlerQueue:self->queue error:&addError]) {
                    [self fail:[self describeError:addError]]; return;
                }
                self->starting = YES;
                [self->stream startCaptureWithCompletionHandler:^(NSError *startError) {
                    dispatch_async(self->queue, ^{
                        self->starting = NO;
                        if (startError) {
                            [self recordFailure:[self describeError:startError]];
                            [self stop];
                            [self stopStream];
                            return;
                        }
                        if (self->stopping) { [self stopStream]; return; }
                        pthread_mutex_lock(&self->mutex);
                        self->ready = YES;
                        pthread_cond_broadcast(&self->condition);
                        pthread_mutex_unlock(&self->mutex);
                    });
                }];
            });
        }];
    });
}
- (void)stream:(SCStream *)sender didStopWithError:(NSError *)error {
    (void)sender;
    dispatch_async(queue, ^{ [self fail:[self describeError:error]]; });
}
- (void)stream:(SCStream *)sender didOutputSampleBuffer:(CMSampleBufferRef)sample ofType:(SCStreamOutputType)type {
    (void)sender;
    if (type != SCStreamOutputTypeAudio || stopping) return;
    @autoreleasepool {
        if (!CMSampleBufferIsValid(sample) || !CMSampleBufferDataIsReady(sample)) return;
        CMItemCount frames = CMSampleBufferGetNumSamples(sample);
        if (frames <= 0) return;
        CMAudioFormatDescriptionRef description = CMSampleBufferGetFormatDescription(sample);
        const AudioStreamBasicDescription *asbd = description ? CMAudioFormatDescriptionGetStreamBasicDescription(description) : NULL;
        if (frames > INT32_MAX || !asbd || asbd->mFormatID != kAudioFormatLinearPCM) {
            [self fail:@"Unsupported native audio sample format."]; return;
        }
        AVAudioFormat *format = [[AVAudioFormat alloc] initWithStreamDescription:asbd];
        AVAudioPCMBuffer *pcm = format ? [[AVAudioPCMBuffer alloc] initWithPCMFormat:format frameCapacity:(AVAudioFrameCount)frames] : nil;
        if (!pcm) { [self fail:@"Cannot allocate native PCM buffer."]; return; }
        pcm.frameLength = (AVAudioFrameCount)frames;
        OSStatus status = CMSampleBufferCopyPCMDataIntoAudioBufferList(sample, 0, (int32_t)frames, pcm.mutableAudioBufferList);
        if (status != noErr) { [self fail:[NSString stringWithFormat:@"Copying native audio failed (OSStatus %d).", (int)status]]; return; }
        if (![sourceFormat isEqual:format]) {
            if (![self convert:nil]) return;
            converter = [[AVAudioConverter alloc] initFromFormat:format toFormat:outputFormat];
            if (!converter) { [self fail:@"Cannot convert native audio to 16000 Hz mono float32."]; return; }
            converter.downmix = YES;
            converter.primeMethod = AVAudioConverterPrimeMethod_None;
            sourceFormat = format;
        }
        [self convert:pcm];
    }
}
@end

vt_audio_capture vt_audio_start(char *error, size_t capacity) {
    @autoreleasepool {
        if ([NSThread isMainThread]) {
            VTErrorCopy(@"Start system audio on a worker goroutine; the macOS main thread must remain free.", error, capacity);
            return NULL;
        }
        if (@available(macOS 13.0, *)) {
            VTAudioCapture *capture = [VTAudioCapture new];
            [capture begin];
            pthread_mutex_lock(&capture->mutex);
            while (!capture->ready && !capture->done && !capture->failure)
                pthread_cond_wait(&capture->condition, &capture->mutex);
            NSString *failure = capture->failure;
            pthread_mutex_unlock(&capture->mutex);
            if (failure) { VTErrorCopy(failure, error, capacity); return NULL; }
            return (__bridge_retained void *)capture;
        }
        VTErrorCopy(@"macOS 13 or newer is required for ScreenCaptureKit system audio.", error, capacity);
        return NULL;
    }
}
ptrdiff_t vt_audio_read(vt_audio_capture handle, float *samples, size_t capacity, char *error, size_t errorCapacity) {
    @autoreleasepool {
        if (!capacity) return 0;
        if ([NSThread isMainThread]) {
            VTErrorCopy(@"Read system audio on a worker goroutine, not the macOS main thread.", error, errorCapacity);
            return -1;
        }
        if (@available(macOS 13.0, *)) {
            VTAudioCapture *capture = (__bridge VTAudioCapture *)handle;
            pthread_mutex_lock(&capture->mutex);
            while (!capture->count && !capture->done) pthread_cond_wait(&capture->condition, &capture->mutex);
            size_t n = MIN(capacity, capture->count);
            size_t first = MIN(n, VT_RING_CAPACITY - capture->head);
            memcpy(samples, capture->ring + capture->head, first * sizeof(float));
            memcpy(samples + first, capture->ring, (n - first) * sizeof(float));
            capture->head = (capture->head + n) % VT_RING_CAPACITY;
            capture->count -= n;
            NSString *failure = capture->failure;
            pthread_mutex_unlock(&capture->mutex);
            if (n) return (ptrdiff_t)n;
            if (failure) { VTErrorCopy(failure, error, errorCapacity); return -1; }
        }
        return 0;
    }
}
int vt_audio_close(vt_audio_capture handle, char *error, size_t capacity) {
    @autoreleasepool {
        if (@available(macOS 13.0, *)) {
            VTAudioCapture *capture = (__bridge VTAudioCapture *)handle;
            // Close can be called from the main thread, but may not wait there.
            [capture requestStop];
            if ([NSThread isMainThread]) {
                VTErrorCopy(@"Capture stop requested; wait for Close on a worker goroutine, not the macOS main thread.", error, capacity);
                return -1;
            }
            pthread_mutex_lock(&capture->mutex);
            while (!capture->done) pthread_cond_wait(&capture->condition, &capture->mutex);
            NSString *failure = capture->failure;
            pthread_mutex_unlock(&capture->mutex);
            if (failure) { VTErrorCopy(failure, error, capacity); return -1; }
        }
        return 0;
    }
}
void vt_audio_release(vt_audio_capture handle) {
    @autoreleasepool {
        if (@available(macOS 13.0, *)) {
            VTAudioCapture *capture = (__bridge_transfer VTAudioCapture *)handle;
            [capture requestStop];
        }
    }
}
