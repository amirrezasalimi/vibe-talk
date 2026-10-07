package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *app) cancelTranslation() {
	if a.translationCancel != nil {
		a.translationCancel()
		a.translationCancel = nil
	}
	a.translationGeneration++
	for i := range a.lines {
		a.lines[i].Translating = false
	}
}

func (a *app) beginTranslation() {
	a.cancelTranslation()
	a.translationContext, a.translationCancel = context.WithCancel(context.Background())
	a.translationSlots = make(chan struct{}, 3)
}

func (a *app) translateLine(index int) {
	if a.closing || !a.translateEnabled || a.translator == nil || a.dispatch == nil {
		return
	}
	if a.translationCancel == nil {
		a.beginTranslation()
	}
	// Do not accumulate an unbounded queue when the endpoint cannot keep up.
	select {
	case a.translationSlots <- struct{}{}:
	default:
		a.lines[index].TranslationError = "Translation busy — line skipped"
		return
	}
	cfg, ctx, slots := a.activeTranslationConfig(), a.translationContext, a.translationSlots
	generation, id, text := a.translationGeneration, a.lines[index].ID, a.lines[index].Text
	a.lines[index].Translating = true
	go func() {
		defer func() { <-slots }()
		result, err := a.translator.Translate(ctx, cfg, text)
		a.dispatch(func() {
			if a.closing || generation != a.translationGeneration {
				return
			}
			// IDs remain unique across clears; late responses never attach to a new row.
			for i := range a.lines {
				if a.lines[i].ID == id {
					a.lines[i].Translating = false
					if err != nil {
						a.lines[i].TranslationError = err.Error()
					} else {
						a.lines[i].Translation = result
					}
					a.refreshOverlay()
					break
				}
			}
		})
	}()
}

func (a *app) modelsForActive() []string {
	if a.modelsByProfile == nil {
		return nil
	}
	return a.modelsByProfile[a.activeProfileID]
}

func (a *app) setModelsForActive(models []string) {
	if a.modelsByProfile == nil {
		a.modelsByProfile = map[string][]string{}
	}
	a.modelsByProfile[a.activeProfileID] = models
}

// activeProfileIndex returns the slice index of the active profile.
func (a *app) activeProfileIndex() int {
	_, idx := a.activeProfile()
	return idx
}

func (a *app) fetchModels() {
	if a.loadingModels {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.modelCancel = cancel
	a.loadingModels, a.translationError = true, ""
	cfg := a.activeTranslationConfig()
	profileID := a.activeProfileID
	go func() {
		defer cancel()
		models, err := a.translator.Models(ctx, cfg)
		a.dispatch(func() {
			a.loadingModels = false
			if a.closing {
				return
			}
			if profileID != a.activeProfileID {
				return
			}
			if err != nil {
				a.translationError = err.Error()
				return
			}
			a.setModelsForActive(models)
			a.persist()
			if len(models) == 0 {
				a.translationError = "No models returned; enter a model ID manually."
			}
		})
	}()
}

func (a *app) addProfile() {
	name := strings.TrimSpace(a.newProfileName)
	if name == "" {
		name = fmt.Sprintf("Endpoint %d", len(a.profiles)+1)
	}
	id := fmt.Sprintf("profile-%d", time.Now().UnixNano())
	a.profiles = append(a.profiles, TranslationProfile{
		ID:      id,
		Name:    name,
		BaseURL: "https://api.openai.com/v1",
		Target:  "English",
	})
	a.activeProfileID = id
	a.newProfileName = ""
	a.translationError = ""
	a.confirmDeleteProfile = false
	a.persist()
}

func (a *app) deleteActiveProfile() {
	idx := a.activeProfileIndex()
	if idx < 0 || len(a.profiles) <= 1 {
		return
	}
	a.profiles = append(a.profiles[:idx], a.profiles[idx+1:]...)
	a.activeProfileID = a.profiles[0].ID
	a.translationError = ""
	a.confirmDeleteProfile = false
	if a.translateEnabled {
		if err := a.activeTranslationConfig().Validate(); err != nil {
			a.translateEnabled = false
			a.translationError = err.Error()
		} else {
			a.beginTranslation()
		}
	}
	a.persist()
}

func profileNames(profiles []TranslationProfile) []string {
	names := make([]string, len(profiles))
	for i, p := range profiles {
		names[i] = p.Name
	}
	return names
}

func (a *app) translationView(c *ui.Context) {
	t := c.Theme()
	dsCard(c, func() {
		dsSectionHead(c, "languages", "Translation", "OpenAI-compatible endpoints · many supported", func() {
			if a.translateEnabled {
				ui.Text(c, "ON").FontSize(10).Bold().TextColor(t.Success)
			} else {
				ui.Text(c, "OFF").FontSize(10).Bold().TextColor(t.TextMuted)
			}
		})

		// Enable row.
		before := a.translateEnabled
		if dsToggleRow(c, &a.translateEnabled, "sparkles", "Auto-translate new lines", "Confirmed captions are sent to the active endpoint", false) {
			a.translationError = ""
			if a.translateEnabled {
				if err := a.activeTranslationConfig().Validate(); err != nil {
					a.translateEnabled = false
					a.translationError = err.Error()
				} else {
					a.beginTranslation()
				}
			} else {
				a.cancelTranslation()
			}
			a.persist()
		}
		_ = before

		// Profile picker row.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			names := profileNames(a.profiles)
			selected := ""
			if p, _ := a.activeProfile(); true {
				selected = p.Name
			}
			sel := ui.Select(c, &selected, names)
			sel.Grow(1)
			sel.Tooltip("Active translation endpoint")
			if sel.Changed() {
				for _, p := range a.profiles {
					if p.Name == selected {
						a.activeProfileID = p.ID
						a.translationError = ""
						a.confirmDeleteProfile = false
						if a.translateEnabled {
							if err := a.activeTranslationConfig().Validate(); err != nil {
								a.translateEnabled = false
								a.translationError = err.Error()
							} else {
								a.beginTranslation()
							}
						}
						a.persist()
						break
					}
				}
			}
			add := iconGhostButton(c, "plus", "Add")
			add.Tooltip("Add endpoint")
			if add.Clicked() {
				a.addProfile()
			}
			del := iconGhostButton(c, "trash", "")
			del.Tooltip("Remove active endpoint")
			if len(a.profiles) <= 1 {
				del.Disabled(true)
			}
			if del.Clicked() {
				if a.confirmDeleteProfile {
					a.deleteActiveProfile()
				} else {
					a.confirmDeleteProfile = true
				}
			}
		})
		if a.confirmDeleteProfile {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				iconEl(c, "alert", dsIconSM, t.Danger)
				ui.Text(c, "Remove this endpoint? Click trash again to confirm.").FontSize(11).TextColor(t.Danger).Grow(1)
			})
		}

		idx := a.activeProfileIndex()
		if idx < 0 {
			dsErrorBanner(c, "No translation endpoint configured.")
			return
		}
		// Edit fields inline; persist on change.
		p := &a.profiles[idx]
		editing := a.translateEnabled || a.loadingModels

		if nameInput := ui.TextInput(c, &p.Name).Label("Profile name").FillWidth().Disabled(editing); nameInput.Changed() {
			a.persist()
		}
		if baseInput := ui.TextInput(c, &p.BaseURL).Label("API base URL (include /v1)").FillWidth().Disabled(editing); baseInput.Changed() {
			a.persist()
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				if keyInput := ui.TextInput(c, &p.APIKey).Label("API key · optional for local servers").Password().FillWidth().Disabled(editing); keyInput.Changed() {
					a.persist()
				}
			})
			loadBtn := iconButton(c, "refresh", func() string {
				if a.loadingModels {
					return "Loading…"
				}
				return "Load models"
			}(), false)
			loadBtn.Tooltip("Fetch /models from this endpoint (60s timeout)")
			if a.loadingModels {
				loadBtn.Disabled(true)
			}
			if loadBtn.Clicked() {
				a.fetchModels()
			}
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				if modelInput := ui.Autocomplete(c, &p.Model, a.modelsForActive()).Label("Model · search or enter ID").FillWidth().Disabled(a.translateEnabled); modelInput.Changed() {
					a.persist()
				}
			})
			ui.Column(c).Width(150).Children(func() {
				if targetInput := ui.TextInput(c, &p.Target).Label("Translate into").FillWidth().Disabled(a.translateEnabled); targetInput.Changed() {
					a.persist()
				}
			})
		})
		if a.loadingModels {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Fetching model list… routers can take up to a minute.").FontSize(11).TextColor(t.TextMuted)
			})
		}
		if a.translationError != "" {
			dsErrorBanner(c, a.translationError)
		}
		modelCount := len(a.modelsForActive())
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			iconEl(c, "shield", dsIconSM, t.TextMuted)
			ui.Text(c, fmt.Sprintf("%d profiles · %d models on active · saved to ~/.vibe-talk/config.yaml", len(a.profiles), modelCount)).FontSize(11).TextColor(t.TextMuted)
		})
		dsNoteBanner(c, "globe", "Only confirmed lines are translated. Remote endpoints may charge per request and retain text. Use a local server (http://localhost:…) to stay fully offline.")
	})
}
