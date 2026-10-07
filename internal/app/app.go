package app

import (
	"context"
	"strings"
	"time"
	"vibe-talk/internal/transcription"
	"vibe-talk/internal/translation"

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
	profiles                                                              []TranslationProfile
	activeProfileID                                                       string
	modelsByProfile                                                       map[string][]string
	translator                                                            *translation.Client
	translateEnabled, translationSettings, captureSettings, loadingModels bool
	translationError                                                      string
	translationCancel                                                     context.CancelFunc
	translationContext                                                    context.Context
	translationSlots                                                      chan struct{}
	translationGeneration                                                 uint64
	nextLineID                                                            uint64
	modelCancel                                                           context.CancelFunc
	language, committed, pending, detected, status, errorText             string
	passMS                                                                float64
	hasMetric, running, stopping, closing                                 bool
	transcriptList                                                        ui.ListState
	lowCPU                                                                bool
	includeMicrophone, noiseFilter                                        bool
	noiseFloorDB                                                          float64
	worker                                                                workerController
	dispatch                                                              func(func())
	closeWindow                                                           func()
	lines                                                                 []captionLine
	sessionStarted                                                        time.Time
	overlayEnabled, clickThrough                                          bool
	overlay                                                               *captionOverlay
	newProfileName                                                        string
	confirmDeleteProfile                                                  bool
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
	if configurable, ok := a.worker.(interface{ SetLowCPU(bool) }); ok {
		configurable.SetLowCPU(a.lowCPU)
	}
	if configurable, ok := a.worker.(interface{ SetInputOptions(bool, float64) }); ok {
		floor := 0.0
		if a.noiseFilter {
			floor = a.noiseFloorDB
			if floor == 0 {
				floor = -45
			}
		}
		configurable.SetInputOptions(a.includeMicrophone, floor)
	}
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
	a.cancelTranslation()
	if a.modelCancel != nil {
		a.modelCancel()
	}
	if !a.running {
		return true
	}
	a.stop()
	return false
}

func (a *app) apply(e transcription.Event) {
	visibleChange := e.Type != "transcript" || e.Text != "" || e.Pending != a.pending
	if visibleChange {
		defer a.refreshOverlay()
	}
	switch e.Type {
	case "status":
		if !a.stopping {
			a.status = e.Message
		}
	case "transcript":

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
	a.cancelTranslation()
	if a.translateEnabled {
		a.beginTranslation()
	}
	a.refreshOverlay()
	a.transcriptList = ui.ListState{FollowEnd: true}
}

func newApp() *app {
	cfg := LoadConfig()
	a := &app{
		worker:          &transcription.Worker{},
		translator:      translation.NewClient(),
		modelsByProfile: map[string][]string{},
	}
	a.applyConfig(cfg)
	// Backfill cached models map from nothing; discovery repopulates.
	if a.modelsByProfile == nil {
		a.modelsByProfile = map[string][]string{}
	}
	return a
}

func Run() error {
	a := newApp()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "vibe-talk", Width: 780, Height: 700, MinWidth: 640, MinHeight: 520,
			StateKey: "main", Content: ui.View(a.view),
		})
		a.overlay = newCaptionOverlay(a.overlayView)
		if a.clickThrough {
			a.overlay.SetClickThrough(true)
		}
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
