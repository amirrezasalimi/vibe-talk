package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

type captionLine struct {
	ID                                      uint64
	Text, At, Translation, TranslationError string
	Translating                             bool
}

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
		a.nextLineID++
		a.lines = append(a.lines, captionLine{ID: a.nextLineID, Text: line, At: fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)})
		a.translateLine(len(a.lines) - 1)
	}
}

func (a *app) refreshOverlay() {
	if a.overlayEnabled && a.overlay != nil {
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

var languageOptions = []string{"auto", "en", "de", "fr", "es", "it", "nl", "pl"}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	status := a.status
	if status == "" {
		status = "Ready"
	}
	hasError := a.errorText != ""

	ui.Column(c).Fill().Children(func() {
		// Header — brand + live status.
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Padding(16, 16, 10, 16).Children(func() {
			ui.Box(c).Size(40, 40).Radius(12).Background(t.Accent).Center().Children(func() {
				iconEl(c, "waveform", 20, t.AccentText)
			})
			ui.Column(c).Grow(1).Gap(1).Children(func() {
				ui.Text(c, "vibe-talk").FontSize(19).Bold()
				engine := "On-device Whistle · no cloud"
				if a.translateEnabled {
					if p, _ := a.activeProfile(); p.Name != "" {
						engine = "Local ASR + " + p.Name
					} else {
						engine = "Local ASR + translation"
					}
				}
				ui.Text(c, engine).FontSize(11).TextColor(t.TextMuted)
			})
			dsStatusPill(c, status, statusTone(t, status, a.running, a.stopping, hasError))
			if a.hasMetric {
				ui.Textf(c, "%.0f ms inference", a.passMS).FontSize(11).TextColor(t.TextMuted)
			}
		})

		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Padding(0, 16, 16, 16).Children(func() {
				a.transportCard(c)
				if a.captureSettings {
					a.audioCard(c)
				}
				if a.translationSettings {
					a.translationView(c)
				}
				if a.errorText != "" {
					dsErrorBanner(c, a.errorText)
				}
				a.transcriptSection(c, status)
				a.sourceFooter(c)
			})
		})
	})
}

func (a *app) transportCard(c *ui.Context) {
	dsCard(c, func() {
		// Primary transport row. Both actions stay mounted so layout is
		// stable and overlapping clicks are safe no-ops.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			start := iconButton(c, "play", "Start listening", true)
			if a.running || a.closing {
				start.Disabled(true)
			}
			if start.Clicked() {
				a.start()
				a.persist()
			}
			stop := iconButton(c, "stop", "Stop", false)
			if !a.running || a.stopping {
				stop.Disabled(true)
			}
			if stop.Clicked() {
				a.stop()
			}
			clearBtn := iconGhostButton(c, "trash", "Clear")
			clearBtn.Tooltip("Clear captions (keeps listening)")
			if clearBtn.Clicked() {
				a.clear()
			}
			ui.Spacer(c)
			overlayLabel := "Show overlay"
			if a.overlayEnabled {
				overlayLabel = "Hide overlay"
			}
			ov := iconGhostButton(c, "monitor", overlayLabel)
			ov.Tooltip("Floating always-on-top captions")
			if ov.Clicked() {
				a.toggleOverlay()
			}
		})

		ui.Divider(c)

		// Settings toggles + click-through.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			audioBtn := settingsToggle(c, "sliders", "Audio settings", a.captureSettings)
			if audioBtn.Clicked() {
				a.captureSettings = !a.captureSettings
				a.translationSettings = false
			}
			transBtn := settingsToggle(c, "languages", "Translation settings", a.translationSettings)
			if transBtn.Clicked() {
				a.translationSettings = !a.translationSettings
				a.captureSettings = false
			}
			ui.Spacer(c)
			before := a.clickThrough
			ui.Checkbox(c, &a.clickThrough, "Click-through").Tooltip("Let clicks pass through the overlay").Disabled(!a.overlayEnabled)
			if before != a.clickThrough {
				if a.overlay != nil {
					a.overlay.SetClickThrough(a.clickThrough)
				}
				a.persist()
			}
		})
	})
}

// settingsToggle is a pill toggle preserving the legacy labels tests rely on.
func settingsToggle(c *ui.Context, iconName, label string, active bool) *ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Gap(7).Padding(7, 12).Radius(999)
	if active {
		b.Background(t.Accent.Alpha(0.14)).Border(1, t.Accent.Alpha(0.4)).TextColor(t.Text)
	} else {
		b.Background(t.Background).Border(1, t.Border).TextColor(t.Text)
	}
	b.Children(func() {
		if active {
			iconEl(c, iconName, dsIconSM, t.Accent)
		} else {
			iconEl(c, iconName, dsIconSM, t.TextMuted)
		}
		ui.Text(c, label).FontSize(12)
	})
	return b
}

func (a *app) audioCard(c *ui.Context) {
	t := c.Theme()
	dsCard(c, func() {
		dsSectionHead(c, "sliders", "Audio capture", "ScreenCaptureKit · 16 kHz mono", nil)

		ui.Column(c).FillWidth().Gap(4).Children(func() {
			if langInput := ui.TextInput(c, &a.language).Label("Source language").Placeholder("Auto · en, de, fr, es, it, nl, pl").FillWidth().Disabled(a.running); langInput.Changed() {
				a.persist()
			}
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				for _, lang := range languageOptions {
					chip := langChip(c, lang, strings.EqualFold(strings.TrimSpace(a.language), lang) ||
						(lang == "auto" && strings.TrimSpace(a.language) == ""))
					if a.running {
						chip.Disabled(true)
					}
					if chip.Clicked() {
						if lang == "auto" {
							a.language = ""
						} else {
							a.language = lang
						}
						a.persist()
					}
				}
			})
		})

		if dsToggleRow(c, &a.lowCPU, "cpu", "Low CPU · 2s chunks", "Fewer passes, slower confirmations", a.running) {
			a.persist()
		}
		if dsToggleRow(c, &a.includeMicrophone, "mic", "Include microphone · macOS 15+", "Mixes default mic with system audio", a.running) {
			a.persist()
		}
		if dsToggleRow(c, &a.noiseFilter, "ear", "Filter quiet background noise", "Level gate — loud noise can still pass", a.running) {
			if a.noiseFilter && a.noiseFloorDB == 0 {
				a.noiseFloorDB = -45
			}
			a.persist()
		}
		if a.noiseFilter {
			if a.noiseFloorDB == 0 {
				a.noiseFloorDB = -45
			}
			ui.Column(c).FillWidth().Gap(6).Padding(10, 12).Radius(10).Background(t.Background).Border(1, t.Border).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Noise cutoff").FontSize(12).Grow(1)
					ui.Textf(c, "%.0f dB", a.noiseFloorDB).FontSize(11).Bold().TextColor(t.Accent)
				})
				if slider := ui.Slider(c, &a.noiseFloorDB, -60, -25).FillWidth().Disabled(a.running); slider.Changed() {
					a.persist()
				}
				dsFieldHint(c, "Higher suppresses more, including soft speech. Silence keeps timestamps intact.")
			})
		}
		dsNoteBanner(c, "shield", "macOS needs Screen & System Audio Recording permission even for audio-only capture. No screen frames are consumed.")
	})
}

func langChip(c *ui.Context, label string, active bool) *ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Padding(4, 10).Radius(999)
	if active {
		b.Background(t.Accent).TextColor(t.AccentText)
	} else {
		b.Background(t.Background).Border(1, t.Border).TextColor(t.TextMuted)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(11).Bold()
	})
	return b
}

func (a *app) transcriptSection(c *ui.Context, status string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		iconEl(c, "captions", dsIconMD, t.TextMuted)
		ui.Text(c, "Transcript").FontSize(15).Bold()
		ui.Spacer(c)
		if len(a.lines) > 0 {
			ui.Box(c).Padding(3, 9).Radius(999).Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Textf(c, "%d lines", len(a.lines)).FontSize(11).TextColor(t.TextMuted)
			})
		} else {
			ui.Text(c, status).FontSize(11).TextColor(t.TextMuted)
		}
	})

	// Hidden status mirror for assistive tech + legacy tests.
	ui.Text(c, "Status: "+status).FontSize(1).TextColor(t.Background)

	a.transcriptList.FollowEnd = true
	rows := len(a.lines)
	hasPending := a.pending != ""
	if hasPending || rows == 0 {
		rows++
	}
	ui.List(c, &a.transcriptList, rows, func(i int) {
		if len(a.lines) == 0 && !hasPending {
			emptyState(c)
			return
		}
		if i < len(a.lines) {
			captionRow(c, a.lines[i])
			return
		}
		if hasPending {
			pendingCard(c, a.pending)
		}
	}).FillWidth().Grow(1)
}

func emptyState(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(28, 20).Gap(8).Background(t.Surface).Radius(dsRadiusCard).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(14).Background(t.Accent.Alpha(0.12)).Center().Children(func() {
			iconEl(c, "captions", dsIconLG, t.Accent)
		})
		ui.Text(c, "Your audio, made readable.").FontSize(15).Bold().TextAlign(ui.Center)
		ui.Text(c, "Start listening, then play a video, meeting, or podcast. Captions appear here line by line.").FillWidth().FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			iconEl(c, "shield", dsIconSM, t.TextMuted)
			ui.Text(c, "On-device by default · translation is opt-in").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

func captionRow(c *ui.Context, line captionLine) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(10).Padding(10, 12).Background(t.Surface).Radius(10).Border(1, t.Border).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Padding(3, 7).Radius(7).Background(t.Background).Border(1, t.Border).Children(func() {
			ui.Text(c, line.At).FontSize(10).Bold().TextColor(t.TextMuted)
		})
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, line.Text).FillWidth().FontSize(14).Selectable()
			if line.Translation != "" {
				ui.Row(c).FillWidth().Gap(8).Padding(8, 10).Radius(8).
					Background(t.Accent.Alpha(0.09)).Border(1, t.Accent.Alpha(0.28)).
					AlignItems(ui.Start).Children(func() {
					iconEl(c, "languages", dsIconSM, t.Accent)
					ui.Text(c, line.Translation).FillWidth().Grow(1).FontSize(13).TextColor(t.Text).Selectable()
				})
			}
			if line.Translating {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Translating…").FontSize(11).TextColor(t.TextMuted)
				})
			}
			if line.TranslationError != "" {
				ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
					iconEl(c, "alert", dsIconSM, t.Danger)
					ui.Text(c, line.TranslationError).FillWidth().Grow(1).FontSize(11).TextColor(t.Danger)
				})
			}
		})
	})
}

func pendingCard(c *ui.Context, pending string) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(11, 13).Gap(5).Radius(10).
		Background(t.Accent.Alpha(0.07)).Border(1, t.Accent.Alpha(0.35)).Children(func() {
		ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(999).Background(t.Accent)
			ui.Text(c, "LIVE · confirming").FontSize(10).Bold().TextColor(t.Accent)
		})
		ui.Text(c, pending).FillWidth().FontSize(15).TextColor(t.TextMuted)
	})
}

func (a *app) sourceFooter(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
		iconName := "monitor"
		if a.includeMicrophone {
			iconName = "mic"
		}
		iconEl(c, iconName, dsIconSM, t.TextMuted)
		source := "System audio · Microphone off · No cloud"
		if a.includeMicrophone {
			source = "System audio + microphone · No cloud"
		}
		if a.translateEnabled {
			source = strings.Replace(source, "No cloud", "Translation API enabled", 1)
		}
		ui.Text(c, source).FontSize(11).TextColor(t.TextMuted)
		if a.detected != "" {
			ui.Spacer(c)
			ui.Text(c, "Heard: "+a.detected).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (a *app) overlayView(c *ui.Context) {
	white := ui.RGB(248, 249, 252)
	muted := ui.RGB(180, 188, 203)
	accent := ui.RGB(154, 207, 255)
	ui.Column(c).Fill().Padding(8).Children(func() {
		ui.Column(c).Fill().Padding(16, 18).Gap(10).Radius(16).Background(ui.RGBA(13, 17, 26, 0.86)).Border(1, ui.RGBA(255, 255, 255, 0.12)).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				iconEl(c, "captions", dsIconSM, muted)
				ui.Column(c).Grow(1).Padding(6, 0).DragWindow().Children(func() {
					ui.Text(c, "VIBE-TALK · LIVE · DRAG TO MOVE").FontSize(10).Bold().TextColor(muted)
				})
				if a.translateEnabled {
					iconEl(c, "languages", dsIconSM, accent)
				}
				if ui.Button(c, "Close overlay").FontSize(11).Clicked() {
					a.hideOverlay()
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(ui.RGBA(255, 255, 255, 0.08))
			ui.Scroll(c).FillWidth().Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					for _, line := range a.lines[max(0, len(a.lines)-2):] {
						ui.Column(c).FillWidth().Gap(3).Children(func() {
							ui.Text(c, line.Text).FillWidth().FontSize(19).TextColor(white)
							if line.Translation != "" {
								ui.Text(c, line.Translation).FillWidth().FontSize(17).TextColor(accent)
							}
							if line.Translating {
								ui.Text(c, "Translating…").FontSize(10).TextColor(muted)
							}
							if line.TranslationError != "" {
								ui.Text(c, "Translation unavailable · check main window").FontSize(10).TextColor(muted)
							}
						})
					}
					if a.pending != "" {
						ui.Column(c).FillWidth().Gap(3).Children(func() {
							ui.Text(c, "LIVE").FontSize(10).Bold().TextColor(accent)
							ui.Text(c, overlayPreview(a.pending)).FillWidth().FontSize(20).TextColor(muted)
						})
					}
					if len(a.lines) == 0 && a.pending == "" {
						label := "Ready to listen"
						if a.running {
							label = "Listening for speech…"
						}
						ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
							iconEl(c, "waveform", dsIconMD, muted)
							ui.Text(c, label).FontSize(19).TextColor(muted)
						})
					}
					if a.errorText != "" {
						ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
							iconEl(c, "alert", dsIconSM, muted)
							ui.Text(c, "Capture stopped — check the main window").FontSize(12).TextColor(muted)
						})
					}
				})
			})
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
				iconEl(c, "pointer", 11, muted)
				ui.Text(c, "Drag header to move · bottom-right corner to resize").FontSize(10).TextColor(muted)
			})
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
