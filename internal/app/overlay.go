package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// captionOverlay owns a separate window, not the caption state. Construct it
// during App.WhenReady (or later), and call its methods on the UI thread.
// The parent view draws the rounded translucent panel and reads UI-owned state.
type captionOverlay struct {
	win *mygo.Window
}

func newCaptionOverlay(view func(*ui.Context)) *captionOverlay {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "vibe-talk captions",
		Width:           800,
		Height:          240,
		MinWidth:        520,
		MinHeight:       160,
		DisableResize:   false,
		Transparent:     true,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableShadow:   true,
		DisableClose:    true,
		DisableMinimize: true,
		DisableMaximize: true,
		StateKey:        "caption-overlay",
		Content: ui.View(func(c *ui.Context) {
			// A transparent native window alone is insufficient: UI paints
			// the theme background at its root. Keep the system theme intact.
			theme := *c.Theme()
			theme.Background = ui.Color{}
			c.SetTheme(&theme)
			if view != nil {
				view(c)
			}
		}),
	})
	configureCaptionOverlay(win.NativeHandle())
	return &captionOverlay{win: win}
}

// Show orders the overlay forward without activating the application.
func (o *captionOverlay) Show() {
	if o != nil && o.win != nil {
		o.win.ShowInactive()
	}
}

func (o *captionOverlay) Hide() {
	if o != nil && o.win != nil {
		o.win.Hide()
	}
}

// Update invalidates the overlay after the parent changes shared caption state.
// It does not mutate that state or synchronously render a frame.
func (o *captionOverlay) Update() {
	if o != nil && o.win != nil {
		o.win.Update(func() {})
	}
}

// SetClickThrough disables mouse interaction, including background dragging.
// Mouse interaction is enabled initially; the parent chooses when to lock it.
func (o *captionOverlay) SetClickThrough(enabled bool) {
	if o != nil && o.win != nil {
		o.win.SetIgnoreMouseEvents(enabled)
	}
}

// Close is terminal and idempotent. To show captions again, create a new overlay.
func (o *captionOverlay) Close() {
	if o != nil && o.win != nil {
		o.win.Close()
		o.win = nil
	}
}
