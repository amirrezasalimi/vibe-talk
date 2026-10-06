# vibe-talk

Live, local transcription of Mac system output with Cactus Whistle. **No Python, interpreter, capture executable, or inference subprocess.** Both audio capture and speech inference run inside the Go application's process.

## Run

Requires Apple Silicon macOS 13+ for capture, Go matching `go.mod`, cgo enabled (default), and Apple's Command Line Tools (`xcode-select --install`). Development builds use the toolchain's default macOS deployment target rather than mixing macOS 13 linker flags with newer object files. To distribute for an older OS, set `MACOSX_DEPLOYMENT_TARGET` consistently for the entire build and verify your Go/toolchain supports that OS; a linker flag alone does not guarantee compatibility.

```sh
sh scripts/setup-transcription.sh
go run .
```

Setup uses curl and clang++, downloading checksum-verified, revision-pinned native engine assets and the 16.9 MB model. Assets have already been installed in this workspace. Setup can be rerun safely. No network access is needed for inference.

Click **Start listening** and play speech in another app. If permission is missing, enable the launching terminal/app in **System Settings → Privacy & Security → Screen & System Audio Recording** (Screen Recording on older macOS), then relaunch. The app reports missing permission rather than prompting automatically. No microphone permission is needed unless you enable **Include microphone**. Protected content may not be capturable.

Languages: English `en`, German `de`, French `fr`, Spanish `es`, Italian `it`, Dutch `nl`, Polish `pl`. Blank means automatic detection.

## Auto-translation (optional)

Open **Translation settings**, enter your OpenAI-compatible **API base URL** (including `/v1` where required) and API key, then click **Load models**. The model field supports autocomplete with keyboard arrows/Enter, or a manually entered model ID for providers without a models endpoint. Set **Translate into** (for example, French) and enable **Auto-translate new lines**. Disable translation to edit its configuration. Model discovery allows 60 seconds for routers that aggregate provider lists; live translation retains its separate 20-second timeout.

Each new confirmed caption segment is translated asynchronously and displayed beneath its original text, including in the overlay. Provisional text is not sent. Capture never waits for the API; up to three requests run concurrently with a 20-second timeout. If the endpoint cannot keep up, additional rows show a busy/skipped message instead of accumulating an unbounded queue. Errors appear on the affected row. Turning translation off, clearing captions, or closing cancels pending translations; old responses cannot attach to new rows. Translation speed depends on your provider/model/network, not Whistle's inference timing. Existing history is not automatically translated.

Transcription remains on-device. **Enabling translation sends confirmed caption text to your chosen server**, which may retain it and charge per request. Use a local OpenAI-compatible server to keep translation local as well. Remote endpoints require HTTPS; HTTP is accepted for localhost only. Credentials are not forwarded on redirects or written to disk. Configuration and the masked API key are session-only and must be reentered after restart. No Python is involved.

The compact main window keeps **Audio settings** and **Translation settings** collapsed by default so captions have more room. Open Audio settings to change language, microphone, CPU mode or noise cutoff.

## Input and noise controls

The system-audio source uses ScreenCaptureKit: macOS requires Screen & System Audio Recording permission even though this app only registers audio outputs and consumes no screen frames. It does not save a desktop recording.

Enable **Include microphone · macOS 15+** before starting to mix your default microphone with app playback. macOS 13/14 continue to support system audio but cannot use this microphone option. The microphone stream has its own persistent resampler; system audio clocks the mixed output with a bounded microphone lead. This is a simple live mix, not sample-perfect multitrack synchronization, speaker separation, or echo cancellation. Use headphones to avoid hearing/transcribing playback again through your mic. Stop and restart to change input settings.

Microphone permission is separate. The bundled app declares `NSMicrophoneUsageDescription` and can request access; a bare `go run .` executable without bundle metadata reports instructions if permission has not already been granted. Use the bundled app or grant access to the launching terminal under Privacy & Security → Microphone.

Enable **Filter quiet background noise** to show a cutoff slider, from -60 dB (gentler) to -25 dB (stronger), initially -45 dB. If every 20 ms window in an inference chunk falls below the cutoff, the chunk is replaced with silence—not skipped—so stream timestamps and confirmation remain intact. This is a conservative level gate, not a trained voice-activity detector: loud noise/music can still produce false text, and too high a cutoff can suppress soft speech. Both microphone input and filtering default to off.

## Floating captions and line-by-line UI

Click **Show overlay** for a frameless, transparent, always-on-top caption widget. Drag its header to position it, and drag the visible bottom-right grip to resize it on macOS; position and size are remembered. The translucent panel fills the window, apart from an eight-point outer margin, so there is no large invisible area underneath. Captions scroll at smaller sizes. Enable **Click-through** after positioning to let mouse input reach the app underneath. Disable it from the main window to drag, resize, or use its **Close overlay** button again. **Close overlay** and the main window's **Hide overlay** both hide the widget without stopping transcription or clearing captions. Reopen it with **Show overlay**.

The overlay shows the two latest confirmed caption rows and a muted live preview. The main window keeps timestamped caption rows, an empty-state guide, separate provisional text, and visible error feedback. Each confirmed streaming delta creates a row; long deltas wrap into word-boundary segments. These are caption updates, not guaranteed complete sentences. Timestamps are relative to the current listening session. It follows new rows only when already scrolled to the bottom.

On macOS the overlay joins all Spaces and is configured as a full-screen auxiliary floating window. Full-screen visibility is best-effort, not a Discord-style game injection: exclusive games and higher-level windows may cover it. Showing it does not activate the app. Interactive dragging/clicking may focus it; click-through avoids this. The overlay has a translucent dark backdrop for readable captions, not an opaque window background.

## Project structure

```text
main.go                        Application entry point (go run .)
internal/
  app/                         UI state, captions, overlay and UI tests
  transcription/               Native audio/overlay bridges, Whistle, sessions and tests
  translation/                 OpenAI-compatible HTTP client and API tests
resources/                     Bundled app resources (icon)
runtime/                       Generated engine library and model weights
scripts/                       Native asset setup
.cache/whistle/                Regenerable setup-only static library cache
```

Go and Objective-C bridge files stay together in their owning package so cgo compiles them correctly. Tests live beside the code they exercise. `internal/app` depends on `internal/transcription`, not the reverse. Generated runtime assets and caches are ignored by Git.

## Native integration

- `internal/transcription/audio_darwin.m` / `internal/transcription/audio_darwin.go`: in-process ScreenCaptureKit via Objective-C/cgo, persistent AVAudioConverter, 16 kHz mono float samples. Audio only: no screen frames are consumed. An eight-second ring buffer fails explicitly on overflow instead of silently dropping audio.
- `internal/transcription/whistle_darwin.go`: direct native C API calls using the existing purego dependency. Loads `libwhistle.dylib` into this process and memory-maps `whistle.cact`. Model and engine stay loaded across listening sessions.
- `internal/transcription/transcription.go`: serializes native stream sessions, frames audio into approximately one-second chunks, and flushes the final tail on Stop.
- `internal/app/`: Start/Stop, caption UI, floating overlay, language, and model-pass timing.
- `main.go`: minimal entry point calling `app.Run()`.

Whistle's API is process-global and non-thread-safe, so the app permits only one inference session at a time. Stop does not kill a process: it closes capture, drains captured audio and flushes Whistle. An in-progress native inference call finishes before shutdown; there is no unsafe forced thread cancellation.

The model is an app resource, not a pure-Go implementation or a single-file executable. The native library and weights are loaded directly; there is no external model runner. Inference makes no download requests and this integration does not invoke Python's usage telemetry.

## CPU usage and latency

Enable **Low CPU mode** before starting a session to feed two-second chunks instead of one-second chunks. This halves inference-call frequency, but captions and confirmation arrive less frequently; actual CPU savings depend on speech and hardware. No audio is skipped. Stop still transcribes the remaining partial chunk and flushes the stream. The model stays loaded, its JSON output buffer is reused, and transcript history uses a virtualized list that builds only visible rows. Hidden overlays are not refreshed; inference-only changes do not redraw the overlay.

The macOS header uses a native drag area that accepts the first mouse press even while the overlay is inactive. The native area reserves the right side for the close button; click-through must still be disabled to move or resize.

One-second chunks follow Whistle's live API guidance. Pending text is provisional; committed words are confirmed by successive passes. `pass_ms` measures engine processing, **not end-to-end caption delay**. The published 11 ms benchmark does not include live buffering and confirmation. Clear clears displayed text, not the engine's current stream; Stop/restart starts a fresh stream. Audio is not saved.

## Assets and packaging

Runtime discovery checks `VIBE_TALK_RUNTIME`, `runtime/` beside the executable, `.app/Contents/Resources/runtime/`, and the working directory's `runtime/` or `resources/runtime/`.

For a MyGo app bundle, copy only `runtime/libwhistle.dylib` and `runtime/whistle.cact` into `resources/runtime/` **before** building. MyGo copies resources into the bundle. Keep the engine's license with redistributed assets (`Cactus-Compute/needle3` publishes an Apache-2.0 LICENSE). Production distribution still needs normal app signing/notarization, including the dylib; no signing pipeline is added here. The static archive is a setup input cached in `.cache/whistle/`, not a runtime requirement; no unused header is downloaded. Intel Mac support is not included in this integration.

## Tests

```sh
go test ./...
```

Tests cover UI, languages, native-session framing, stop/drain/flush, overlapping session rejection, noise gating, translation API payloads/model lists/errors/cancellation and stale response handling. A separate opt-in native smoke test loads the real engine and transcribes synthetic silence without capturing screen/audio:

```sh
VIBE_TALK_NATIVE_TEST=1 go test ./internal/transcription -run TestWhistleNative -v
```

Live playback capture, permission flow and transcription accuracy require testing interactively on a Mac.
