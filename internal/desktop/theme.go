package desktop

import "github.com/egoist/mygo/ui"

// Keep scroll indicators light in desktop panels while retaining the native
// scrolling, keyboard navigation and pointer hit targets.
func panelTheme(c *ui.Context) {
	theme := *c.Theme()
	theme.ScrollbarWidth = 3
	theme.Scrollbar = theme.Text.Alpha(.5)
	c.SetTheme(&theme)
}
