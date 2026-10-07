package app

import (
	"github.com/egoist/mygo/ui"
)

// Design system: single place for spacing, type scale and composite
// primitives so every screen shares one visual language.

const (
	dsRadiusCard = 12
	dsRadiusPill = 999
	dsIconSM     = 14
	dsIconMD     = 16
	dsIconLG     = 20
	dsAvatarSize = 36
	dsHeaderH    = 0 // fluid; kept for future fixed header
)

// dsCard is the standard elevated surface for grouped controls.
func dsCard(c *ui.Context, fn func()) *ui.Element {
	t := c.Theme()
	return ui.Column(c).FillWidth().Padding(14).Gap(10).
		Background(t.Surface).Radius(dsRadiusCard).Border(1, t.Border).
		Children(fn)
}

// dsSectionHead renders an icon + title + optional trailing content.
func dsSectionHead(c *ui.Context, iconName, title, subtitle string, trailing func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(dsAvatarSize, dsAvatarSize).Radius(10).
			Background(t.Accent.Alpha(0.12)).
			Center().Children(func() {
			iconEl(c, iconName, dsIconMD, t.Accent)
		})
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).Bold()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(11).TextColor(t.TextMuted)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}

// dsStatusPill shows engine state with a colored dot.
func dsStatusPill(c *ui.Context, status string, tone ui.Color) {
	t := c.Theme()
	ui.Row(c).Gap(7).AlignItems(ui.Center).
		Padding(5, 11).Radius(dsRadiusPill).
		Background(t.Background).Border(1, t.Border).
		Children(func() {
			ui.Box(c).Size(8, 8).Radius(999).Background(tone)
			ui.Text(c, status).FontSize(11).Bold()
		})
}

// statusTone maps textual status to a dot color.
func statusTone(t *ui.Theme, status string, running, stopping bool, hasError bool) ui.Color {
	switch {
	case hasError:
		return t.Danger
	case stopping:
		return t.Warning
	case running:
		return t.Success
	case status == "Failed":
		return t.Danger
	default:
		return t.TextMuted
	}
}

// dsFieldHint is the small muted helper under an input.
func dsFieldHint(c *ui.Context, text string) {
	ui.Text(c, text).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
}

// dsErrorBanner is the single style for every inline error.
func dsErrorBanner(c *ui.Context, msg string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(9).Padding(10, 12).Radius(10).
		Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.35)).
		AlignItems(ui.Center).Children(func() {
		iconEl(c, "alert", dsIconMD, t.Danger)
		ui.Text(c, msg).FillWidth().Grow(1).FontSize(12).TextColor(t.Danger)
	})
}

// dsNoteBanner is the informational counterpart (privacy, hints).
func dsNoteBanner(c *ui.Context, iconName, msg string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(9).Padding(10, 12).Radius(10).
		Background(t.Accent.Alpha(0.08)).Border(1, t.Accent.Alpha(0.25)).
		AlignItems(ui.Start).Children(func() {
		iconEl(c, iconName, dsIconMD, t.Accent)
		ui.Text(c, msg).FillWidth().Grow(1).FontSize(11).TextColor(t.TextMuted)
	})
}

// iconButton builds a ButtonBase with icon + label and theme styling.
func iconButton(c *ui.Context, iconName, label string, primary bool) *ui.Element {
	b := ui.ButtonBase(c).Gap(8).Padding(8, 14).Radius(c.Theme().Radius)
	t := c.Theme()
	if primary {
		b.Background(t.Accent).TextColor(t.AccentText)
	} else {
		b.Background(t.Background).Border(1, t.Border).TextColor(t.Text)
	}
	b.Children(func() {
		if iconName != "" {
			if primary {
				iconEl(c, iconName, dsIconSM, t.AccentText)
			} else {
				iconEl(c, iconName, dsIconSM, t.TextMuted)
			}
		}
		if label != "" {
			ui.Text(c, label).FontSize(13).Bold()
		}
	})
	return b
}

// iconGhostButton is a borderless action for toolbars.
func iconGhostButton(c *ui.Context, iconName, label string) *ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Gap(7).Padding(7, 11).Radius(t.Radius)
	b.Children(func() {
		iconEl(c, iconName, dsIconSM, t.TextMuted)
		if label != "" {
			ui.Text(c, label).FontSize(12).TextColor(t.Text)
		}
	})
	return b
}

// dsToggleRow renders a switch with title + description (settings list).
// The label area is its own button so clicking the text toggles,
// while the switch stays a separate control (never nested clickables).
func dsToggleRow(c *ui.Context, on *bool, iconName, title, desc string, disabled bool) bool {
	t := c.Theme()
	before := *on
	toggle := func() {
		if !disabled {
			*on = !*on
		}
	}
	ui.Row(c).FillWidth().Gap(11).AlignItems(ui.Center).
		Padding(10, 12).Radius(10).
		Background(t.Background).Border(1, t.Border).
		Children(func() {
			iconEl(c, iconName, dsIconMD, t.TextMuted)
			lbl := ui.ButtonBase(c).Grow(1)
			lbl.Children(func() {
				ui.Column(c).Grow(1).Gap(1).AlignItems(ui.Start).Children(func() {
					ui.Text(c, title).FontSize(13)
					if desc != "" {
						ui.Text(c, desc).FontSize(11).TextColor(t.TextMuted)
					}
				})
			})
			if disabled {
				lbl.Disabled(true)
			}
			if lbl.Clicked() {
				toggle()
			}
			sw := ui.Switch(c, on)
			if disabled {
				sw.Disabled(true)
			}
		})
	return before != *on
}
