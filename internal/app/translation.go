package app

import (
	"context"
	"fmt"
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
	cfg, ctx, slots := a.translationConfig, a.translationContext, a.translationSlots
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
func (a *app) fetchModels() {
	if a.loadingModels {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.modelCancel = cancel
	a.loadingModels, a.translationError = true, ""
	cfg := a.translationConfig
	go func() {
		defer cancel()
		models, err := a.translator.Models(ctx, cfg)
		a.dispatch(func() {
			a.loadingModels = false
			if a.closing {
				return
			}
			if err != nil {
				a.translationError = err.Error()
				return
			}
			a.models = models
			if len(models) == 0 {
				a.translationError = "No models returned; enter a model ID manually."
			}
		})
	}()
}
func (a *app) translationView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(12).Gap(8).Background(t.Surface).Radius(10).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			before := a.translateEnabled
			ui.Checkbox(c, &a.translateEnabled, "Auto-translate new lines")
			if before != a.translateEnabled {
				a.translationError = ""
				if a.translateEnabled {
					if err := a.translationConfig.Validate(); err != nil {
						a.translateEnabled = false
						a.translationError = err.Error()
					} else {
						a.beginTranslation()
					}
				} else {
					a.cancelTranslation()
				}
			}
		})
		ui.TextInput(c, &a.translationConfig.BaseURL).Label("API base URL (include /v1)").FillWidth().Disabled(a.translateEnabled || a.loadingModels)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.TextInput(c, &a.translationConfig.APIKey).Label("API key · optional for local servers").Password().Grow(1).Disabled(a.translateEnabled || a.loadingModels)
			label := "Load models"
			if a.loadingModels {
				label = "Loading…"
			}
			if ui.Button(c, label).Disabled(a.loadingModels).Clicked() {
				a.fetchModels()
			}
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Autocomplete(c, &a.translationConfig.Model, a.models).Label("Model · search or enter ID").Grow(1).Disabled(a.translateEnabled)
			ui.TextInput(c, &a.translationConfig.Target).Label("Translate into").Width(160).Disabled(a.translateEnabled)
		})
		if a.translationError != "" {
			ui.Text(c, a.translationError).FillWidth().FontSize(11).TextColor(t.Danger)
		}
		ui.Text(c, fmt.Sprintf("%d models · settings are session-only · API key is not saved", len(a.models))).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, "Enabled: confirmed caption text goes to this endpoint. Remote APIs may charge per request.").FillWidth().FontSize(11).TextColor(t.TextMuted)
	})
}
