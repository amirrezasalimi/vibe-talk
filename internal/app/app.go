package app

import (
	"math"
	"strings"
	"time"
	"vibe-talk/internal/transcription"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type workerController interface {
	Start(string, func(transcription.Event), func(error)) error
	Stop()
}

// All app state is main-thread owned. The worker only invokes callbacks that
// dispatch state mutations through Window.Update (a deterministic queue in tests).
type app struct {
	language, committed, pending, detected, status, errorText string
	passMS                                                    float64
	hasMetric, running, stopping, closing                     bool
	scroll                                                    ui.ScrollState
	worker                                                    workerController
	dispatch                                                  func(func())
	closeWindow                                               func()
	lines                                                     []captionLine
	sessionStarted                                            time.Time
	overlayEnabled, clickThrough                              bool
	overlay                                                   *captionOverlay
}

func (a *app) start() {
	if a.running || a.closing {
		return
	}
	language, err := transcription.NormalizeLanguage(a.language)
	if err != nil {
		a.errorText = err.Error()
		return
	}
	a.running, a.stopping = true, false
	a.sessionStarted = time.Now()
	a.errorText, a.status, a.pending = "", "Starting…", ""
	a.detected, a.hasMetric = "", false
	err = a.worker.Start(language, func(e transcription.Event) {
		a.dispatch(func() { a.apply(e) })
	}, func(err error) {
		a.dispatch(func() {
			a.running, a.stopping = false, false
			if err != nil {
				a.errorText = err.Error()
			}
			if a.errorText != "" {
				a.status = "Failed"
			} else {
				a.status = "Stopped"
			}
			a.refreshOverlay()
			if a.closing && a.closeWindow != nil {
				a.closeWindow()
			}
		})
	})
	if err != nil {
		a.running = false
		a.status = "Failed"
		a.errorText = err.Error()
	}
}

func (a *app) stop() {
	if !a.running || a.stopping {
		return
	}
	a.stopping, a.status = true, "Stopping…"
	a.worker.Stop()
}

func (a *app) requestClose() bool {
	a.closing = true
	if !a.running {
		return true
	}
	a.stop()
	return false
}

func (a *app) apply(e transcription.Event) {
	defer a.refreshOverlay()
	switch e.Type {
	case "status":
		if !a.stopping {
			a.status = e.Message
		}
	case "transcript":
		if a.scroll.Y >= a.scroll.MaxY {
			a.scroll.Y = math.MaxFloat32
		}
		// Text is a delta, not a replacement or cumulative transcript.
		if a.committed != "" && e.Text != "" && strings.TrimRight(a.committed, " \n\t") == a.committed && strings.TrimLeft(e.Text, " \n\t") == e.Text {
			a.committed += " "
		}
		a.committed += e.Text
		a.appendCaption(e.Text)
		a.pending, a.detected, a.passMS, a.hasMetric = e.Pending, e.Language, e.PassMS, true
	case "error":
		a.errorText = e.Message
	case "done":
		// Keep Start disabled until the session finishes native cleanup.
		if !a.stopping {
			a.status = "Finishing…"
		}
	}
}

func (a *app) clear() {
	a.committed, a.pending = "", ""
	a.lines = nil
	a.refreshOverlay()
	a.scroll = ui.ScrollState{}
}

func Run() error {
	a := &app{worker: &transcription.Worker{}}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "vibe-talk", Width: 840, Height: 680, MinWidth: 680, MinHeight: 580,
			StateKey: "main", Content: ui.View(a.view),
		})
		a.overlay = newCaptionOverlay(a.overlayView)
		a.dispatch = win.Update
		a.closeWindow = win.Close
		win.OnClose(func(e *mygo.CloseEvent) {
			if !a.requestClose() {
				e.PreventDefault()
			}
		})
		// Cmd-Q also waits for the native stream's final transcript and cleanup.
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if !a.requestClose() {
				e.PreventDefault()
				a.closeWindow = mygo.App.Quit
			}
		})
		win.OnClosed(func() {
			a.worker.Stop()
			a.overlay.Close()
		})
	})
	return mygo.App.Run()
}
