package app

import (
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
	"vibe-talk/internal/transcription"
)

func TestCaptionSegments(t *testing.T) {
	lines := captionSegments(" first line\nsecond line ")
	if len(lines) != 2 || lines[0] != "first line" || lines[1] != "second line" {
		t.Fatal(lines)
	}
	text := strings.Repeat("word ", 60)
	lines = captionSegments(text)
	if len(lines) < 2 || strings.Join(lines, " ") != strings.TrimSpace(text) {
		t.Fatal(lines)
	}
	if len(captionSegments(" \n ")) != 0 {
		t.Fatal("blank creates caption")
	}
}
func TestLineByLineCaptions(t *testing.T) {
	a := &app{}
	a.apply(transcription.Event{Type: "transcript", Text: "First caption", Pending: "unfinished"})
	a.apply(transcription.Event{Type: "transcript", Text: "Second caption", Pending: "preview"})
	if len(a.lines) != 2 || a.lines[1].Text != "Second caption" || a.committed != "First caption Second caption" {
		t.Fatalf("%+v", a)
	}
	a.clear()
	if len(a.lines) != 0 || a.pending != "" {
		t.Fatal("clear leaves captions")
	}
}
func TestOverlayControls(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(a.view, 840, 680)
	if err := tt.Click("Show overlay"); err != nil {
		t.Fatal(err)
	}
	if !a.overlayEnabled || !tt.HasText("Hide overlay") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Click-through"); err != nil {
		t.Fatal(err)
	}
	if !a.clickThrough {
		t.Fatal("click-through not enabled")
	}
	if err := tt.Click("Hide overlay"); err != nil {
		t.Fatal(err)
	}
	if a.overlayEnabled {
		t.Fatal("overlay still enabled")
	}
}
func TestCloseOverlayKeepsListening(t *testing.T) {
	worker := &fakeWorker{}
	updates := 0
	a := &app{
		worker: worker, running: true, overlayEnabled: true,
		dispatch: func(f func()) { updates++; f() },
		lines:    []captionLine{{Text: "Keep this caption"}},
	}
	tt := ui.NewTester(a.overlayView, 800, 320)
	if err := tt.Click("Close overlay"); err != nil {
		t.Fatal(err)
	}
	if a.overlayEnabled || !a.running || worker.stops != 0 || len(a.lines) != 1 {
		t.Fatal("closing overlay must only hide it, not stop or clear captions")
	}
	if updates != 1 {
		t.Fatal("main window was not refreshed")
	}
	a.toggleOverlay()
	if !a.overlayEnabled {
		t.Fatal("overlay cannot be reopened")
	}
}

func TestOverlayPreview(t *testing.T) {
	preview := overlayPreview(strings.Repeat("é", 140))
	if len([]rune(preview)) != 121 || !strings.HasPrefix(preview, "…") {
		t.Fatal(preview)
	}
}
