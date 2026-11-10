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

// Indigo-violet palette. Accents keep at least 4.5:1 contrast with white
// text in both schemes.
var (
	indigo600 = ui.Hex("#4F46E5")
	indigo700 = ui.Hex("#4338CA")
	indigo800 = ui.Hex("#3730A3")
	indigo500 = ui.Hex("#6366F1")
	indigo400 = ui.Hex("#818CF8")
	violet500 = ui.Hex("#8B5CF6")
)

// Accent is the brand color for the given scheme, for custom drawing.
func Accent(dark bool) ui.Color {
	if dark {
		return indigo400
	}
	return indigo600
}

// Violet is the gradient's second stop.
func Violet() ui.Color { return violet500 }

// Apply sets the brand theme on c, derived from the system light/dark theme
// so text colors and platform metrics stay native.
func Apply(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		t.Background = ui.Hex("#15151C")
		t.Surface = ui.Hex("#1D1D26")
		t.SurfaceHover = ui.Hex("#262632")
		t.SurfacePressed = ui.Hex("#30303E")
		t.Border = ui.Hex("#34354A")
		t.Accent = ui.Hex("#5B5CEB")
		t.AccentHover = indigo500
		t.AccentPressed = indigo600
		t.Focus = indigo400.Alpha(.75)
		t.Selection = indigo400.Alpha(.35)
	} else {
		t.Background = ui.Hex("#FFFFFF")
		t.Surface = ui.Hex("#F5F5FA")
		t.SurfaceHover = ui.Hex("#ECECF6")
		t.SurfacePressed = ui.Hex("#E1E1EF")
		t.Border = ui.Hex("#DCDCEA")
		t.Accent = indigo600
		t.AccentHover = indigo700
		t.AccentPressed = indigo800
		t.Focus = indigo500.Alpha(.6)
		t.Selection = indigo500.Alpha(.22)
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
		return indigo400.Alpha(.2)
	}
	return indigo500.Alpha(.13)
}

// Ink is accent-colored text or icons on Soft.
func Ink(c *ui.Context) ui.Color { return Accent(c.Theme().Dark) }
