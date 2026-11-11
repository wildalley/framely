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

// Teal palette from the app icon on warm neutrals. Filled accents keep at
// least 4.5:1 contrast with white text; Ink is for glyphs and text on soft
// tints. Signal, the icon's folded-corner orange, marks recording and other
// live states and is never used for text.
var (
	teal300   = ui.Hex("#6FBDB7")
	teal700   = ui.Hex("#0E5A5E")
	fill      = ui.Hex("#0E5A5E") // 7.6:1 with white
	hover     = ui.Hex("#0A4D51")
	press     = ui.Hex("#073D41")
	darkFill  = ui.Hex("#1B7575") // 5.3:1 with white
	darkHover = ui.Hex("#186A6A")
	darkPress = ui.Hex("#145B5B")
	ink       = ui.Hex("#0B5A5F")
	// Signal is the icon's orange, for recording and live markers.
	Signal = ui.Hex("#ED782F")
)

// Accent is the glyph color of the brand mark for the given scheme.
func Accent(dark bool) ui.Color {
	if dark {
		return teal300
	}
	return teal700
}

// Apply sets the brand theme on c, derived from the system light/dark theme
// so text colors and platform metrics stay native.
func Apply(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		t.Background = ui.Hex("#161A1A")
		t.Surface = ui.Hex("#1E2323")
		t.SurfaceHover = ui.Hex("#262C2C")
		t.SurfacePressed = ui.Hex("#2F3636")
		t.Border = ui.Hex("#333B3B")
		t.Focus = teal300.Alpha(.7)
		t.Selection = teal300.Alpha(.3)
		t.Accent, t.AccentHover, t.AccentPressed = darkFill, darkHover, darkPress
	} else {
		t.Background = ui.Hex("#FFFFFF")
		t.Surface = ui.Hex("#F5F3EE")
		t.SurfaceHover = ui.Hex("#EEEBE4")
		t.SurfacePressed = ui.Hex("#E5E1D8")
		t.Border = ui.Hex("#E3DED3")
		t.Focus = teal700.Alpha(.5)
		t.Selection = teal700.Alpha(.2)
		t.Accent, t.AccentHover, t.AccentPressed = fill, hover, press
	}
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
		return teal300.Alpha(.18)
	}
	return teal700.Alpha(.12)
}

// Ink is accent-colored text or icons on Soft.
func Ink(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return teal300
	}
	return ink
}
