package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

type captionLine struct{ Text, At string }

// Each confirmed delta becomes its own readable row. Split unusually long
// deltas at word boundaries so a final flush never makes an enormous card.
func captionSegments(text string) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		line := ""
		for _, word := range words {
			if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > 88 {
				lines = append(lines, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
func (a *app) appendCaption(text string) {
	elapsed := time.Duration(0)
	if !a.sessionStarted.IsZero() {
		elapsed = time.Since(a.sessionStarted)
	}
	seconds := int(elapsed.Seconds())
	for _, line := range captionSegments(text) {
		a.lines = append(a.lines, captionLine{Text: line, At: fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)})
	}
}
func (a *app) refreshOverlay() {
	if a.overlay != nil {
		a.overlay.Update()
	}
}
func (a *app) hideOverlay() {
	a.overlayEnabled = false
	if a.overlay != nil {
		a.overlay.Hide()
	}
	// The close button runs in the overlay's view; invalidate the main
	// window too so its toggle immediately switches to "Show overlay".
	if a.dispatch != nil {
		a.dispatch(func() {})
	}
}

func (a *app) toggleOverlay() {
	if a.overlayEnabled {
		a.hideOverlay()
		return
	}
	a.overlayEnabled = true
	if a.overlay != nil {
		if a.overlayEnabled {
			a.overlay.SetClickThrough(a.clickThrough)
			a.overlay.Show()
		} else {
			a.overlay.Hide()
		}
	}
}
func (a *app) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Gap(18).Padding(24).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "vibe-talk").FontSize(28).Bold()
				ui.Text(c, "Live captions for your Mac").TextColor(t.TextMuted)
			})
			ui.Text(c, "ON DEVICE").FontSize(11).Bold().TextColor(t.Accent)
		})
		ui.Column(c).FillWidth().Padding(16).Gap(12).Background(t.Surface).Radius(14).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c, "Start listening").Disabled(a.running || a.closing).Clicked() {
					a.start()
				}
				if ui.Button(c, "Stop").Disabled(!a.running || a.stopping).Clicked() {
					a.stop()
				}
				if ui.Button(c, "Clear").Clicked() {
					a.clear()
				}
			})
			ui.TextInput(c, &a.language).Label("Source language").Placeholder("Auto-detect · or en, de, fr, es, it, nl, pl").FillWidth().Disabled(a.running)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				label := "Show overlay"
				if a.overlayEnabled {
					label = "Hide overlay"
				}
				if ui.Button(c, label).Clicked() {
					a.toggleOverlay()
				}
				before := a.clickThrough
				ui.Checkbox(c, &a.clickThrough, "Click-through").Disabled(!a.overlayEnabled)
				if before != a.clickThrough && a.overlay != nil {
					a.overlay.SetClickThrough(a.clickThrough)
				}
			})
			ui.Text(c, "Drag the overlay header to move it; Close overlay hides it. Disable click-through to use these controls.").FontSize(12).FillWidth().TextColor(t.TextMuted)
		})
		status := a.status
		if status == "" {
			status = "Ready"
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, "Status: "+status).FillWidth().Grow(1).FontSize(12).TextColor(t.TextMuted)
			if a.hasMetric {
				ui.Textf(c, "%.0f ms inference", a.passMS).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if a.errorText != "" {
			ui.Text(c, a.errorText).FillWidth().Padding(12).Radius(10).Background(t.Surface).TextColor(t.Danger)
		}
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, "Transcript").FontSize(17).Bold().Grow(1)
			ui.Textf(c, "%d lines", len(a.lines)).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&a.scroll).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if len(a.lines) == 0 && a.pending == "" {
					ui.Column(c).FillWidth().Padding(24).Gap(8).Background(t.Surface).Radius(12).Children(func() {
						ui.Text(c, "Your audio, made readable.").FontSize(19).Bold()
						ui.Text(c, "Start listening, then play a video, meeting, or podcast. Captions appear here line by line.").FillWidth().TextColor(t.TextMuted)
					})
				}
				for _, line := range a.lines {
					ui.Row(c).FillWidth().Gap(14).Padding(12).Background(t.Surface).Radius(10).Children(func() {
						ui.Text(c, line.At).Width(48).FontSize(12).TextColor(t.TextMuted)
						ui.Text(c, line.Text).Grow(1).FontSize(17)
					})
				}
				if a.pending != "" {
					ui.Column(c).FillWidth().Padding(12).Gap(4).Radius(10).Border(1, t.Border).Children(func() {
						ui.Text(c, "LIVE · confirming").FontSize(11).TextColor(t.Accent)
						ui.Text(c, a.pending).FillWidth().FontSize(17).TextColor(t.TextMuted)
					})
				}
			})
		})
		ui.Text(c, "System audio only · No microphone · No cloud").FontSize(11).TextColor(t.TextMuted)
	})
}
func (a *app) overlayView(c *ui.Context) {
	white := ui.RGB(248, 249, 252)
	muted := ui.RGB(180, 188, 203)
	ui.Column(c).Fill().Padding(8).Children(func() {
		ui.Column(c).Fill().Padding(18).Gap(10).Radius(16).Background(ui.RGBA(15, 19, 28, 0.82)).Border(1, ui.RGBA(255, 255, 255, 0.12)).Children(func() {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Padding(8, 0).DragWindow().Children(func() {
					ui.Text(c, "VIBE-TALK · LIVE CAPTIONS · DRAG TO MOVE").FontSize(10).Bold().TextColor(muted)
				})
				if ui.Button(c, "Close overlay").FontSize(11).Clicked() {
					a.hideOverlay()
				}
			})
			// Keep the header and resize hint visible at the minimum window size.
			ui.Scroll(c).FillWidth().Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					for _, line := range a.lines[max(0, len(a.lines)-2):] {
						ui.Text(c, line.Text).FillWidth().FontSize(21).TextColor(white)
					}
					if a.pending != "" {
						ui.Text(c, overlayPreview(a.pending)).FillWidth().FontSize(21).TextColor(muted)
					}
					if len(a.lines) == 0 && a.pending == "" {
						label := "Ready to listen"
						if a.running {
							label = "Listening for speech…"
						}
						ui.Text(c, label).FontSize(21).TextColor(muted)
					}
					if a.errorText != "" {
						ui.Text(c, "Capture stopped — check the main window").FontSize(12).TextColor(muted)
					}
				})
			})
			ui.Text(c, "Drag bottom-right corner to resize").FontSize(10).TextColor(muted)
		})
	})
}
func overlayPreview(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 120 {
		return "…" + string(runes[len(runes)-120:])
	}
	return string(runes)
}
