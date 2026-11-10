// Package brand holds the product name and the shared visual theme.
//
// The executable, configuration directory, desktop/portal application ID
// and niri markers keep their historical "screenpin" names so upgrades keep
// settings and permissions; only what people read uses Name.
package brand

import "github.com/egoist/mygo/ui"

const (
	// Name is the product name shown in windows, the tray and dialogs.
	Name = "Framely"
	// ChineseName is shown next to Name where there is room.
	ChineseName = "取景"
)

// Title names a window after one of the product's tools.
func Title(tool string) string {
	if tool == "" {
		return Name
	}
	return Name + " · " + tool
}

// Sky palette on cool neutrals. Filled accents keep at least 4.5:1 contrast
// with white text; Ink is for glyphs and text on soft tints.
var (
	sky400 = ui.Hex("#38BDF8")
	sky500 = ui.Hex("#0EA5E9")
	fill   = ui.Hex("#0277B6") // 5.0:1 with white
	hover  = ui.Hex("#03699F")
	press  = ui.Hex("#035A88")
	ink    = ui.Hex("#0369A1")
)

// Accent is the glyph color of the brand mark for the given scheme.
func Accent(dark bool) ui.Color {
	if dark {
		return sky400
	}
	return sky500
}

// Apply sets the brand theme on c, derived from the system light/dark theme
// so text colors and platform metrics stay native.
func Apply(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		t.Background = ui.Hex("#17191C")
		t.Surface = ui.Hex("#1F2226")
		t.SurfaceHover = ui.Hex("#282C31")
		t.SurfacePressed = ui.Hex("#31363C")
		t.Border = ui.Hex("#34393F")
		t.Focus = sky400.Alpha(.7)
		t.Selection = sky400.Alpha(.3)
	} else {
		t.Background = ui.Hex("#FFFFFF")
		t.Surface = ui.Hex("#F5F7F9")
		t.SurfaceHover = ui.Hex("#ECF0F3")
		t.SurfacePressed = ui.Hex("#E2E7EC")
		t.Border = ui.Hex("#DFE4E9")
		t.Focus = sky500.Alpha(.55)
		t.Selection = sky500.Alpha(.22)
	}
	t.Accent, t.AccentHover, t.AccentPressed = fill, hover, press
	t.AccentText = ui.Hex("#FFFFFF")
	t.InactiveAccent = t.Accent.Alpha(.55)
	if t.Radius < 8 {
		t.Radius = 8
	}
	c.SetTheme(&t)
}

// Soft is a translucent accent fill for selected tools and chips.
func Soft(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return sky400.Alpha(.18)
	}
	return sky500.Alpha(.13)
}

// Ink is accent-colored text or icons on Soft.
func Ink(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return sky400
	}
	return ink
}
