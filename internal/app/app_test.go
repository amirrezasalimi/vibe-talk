package app

import (
	"errors"
	"testing"
	"vibe-talk/internal/transcription"

	"github.com/egoist/mygo/ui"
)

type fakeWorker struct {
	starts, stops int
	language      string
	event         func(transcription.Event)
	finish        func(error)
}

func (w *fakeWorker) Start(language string, event func(transcription.Event), finish func(error)) error {
	w.starts++
	w.language, w.event, w.finish = language, event, finish
	return nil
}
func (w *fakeWorker) Stop() { w.stops++ }

func TestView(t *testing.T) {
	w := &fakeWorker{}
	var queue []func()
	a := &app{worker: w, dispatch: func(f func()) { queue = append(queue, f) }}
	tt := ui.NewTester(a.view, 720, 560)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	flush := func() {
		for _, f := range queue {
			f()
		}
		queue = nil
		tt.Frame()
	}
	if !tt.HasText("Status: Ready") {
		t.Fatal(tt.Texts())
	}
	click("Source language")
	tt.Type("fr")
	click("Start listening")
	if w.starts != 1 || w.language != "fr" || !a.running {
		t.Fatalf("worker: %+v", w)
	}
	click("Start listening")
	if w.starts != 1 {
		t.Fatal("overlapping start")
	}
	w.event(transcription.Event{Type: "transcript", Text: "Bonjour ", Pending: "le", Language: "fr", PassMS: 12.5})
	if a.committed != "" {
		t.Fatal("state changed outside dispatch")
	}
	flush()
	if !tt.HasText("Bonjour") || !tt.HasText("le") || !tt.HasText("12 ms inference") {
		t.Fatal(tt.Texts())
	}
	w.event(transcription.Event{Type: "transcript", Text: "monde", Pending: "!", Language: "fr"})
	flush()
	if a.committed != "Bonjour monde" || a.pending != "!" {
		t.Fatalf("%+v", a)
	}
	click("Clear")
	if a.committed != "" || a.pending != "" || !a.running {
		t.Fatal("Clear must not stop capture")
	}
	click("Stop")
	click("Stop")
	if w.stops != 1 || !a.running || !a.stopping {
		t.Fatal("stop lifecycle")
	}
	w.event(transcription.Event{Type: "transcript", Text: "Final", Pending: ""})
	w.event(transcription.Event{Type: "done"})
	flush()
	if !a.running || a.committed != "Final" {
		t.Fatal("done must not unlock Start before session cleanup")
	}
	w.finish(nil)
	flush()
	if a.running || !tt.HasText("Status: Stopped") {
		t.Fatal(tt.Texts())
	}
	click("Start listening")
	w.finish(errors.New("permission denied"))
	flush()
	if !tt.HasText("permission denied") {
		t.Fatal(tt.Texts())
	}
}

func TestInvalidLanguageAndClose(t *testing.T) {
	w := &fakeWorker{}
	a := &app{worker: w, language: "xx", dispatch: func(f func()) { f() }}
	a.start()
	if a.running || w.starts != 0 || a.errorText == "" {
		t.Fatal("invalid source language accepted")
	}
	a.language = "auto"
	a.start()
	closed := false
	a.closeWindow = func() { closed = true }
	if a.requestClose() || closed || w.stops != 1 {
		t.Fatal("close must wait for worker")
	}
	if a.requestClose() || w.stops != 1 {
		t.Fatal("duplicate stop")
	}
	w.finish(nil)
	if !closed || !a.requestClose() {
		t.Fatal("close not completed")
	}
	a.start()
	if w.starts != 1 {
		t.Fatal("started while closing")
	}
}
