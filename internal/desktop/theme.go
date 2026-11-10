package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/assets"
	"github.com/wildalley/screenpin/internal/brand"
)

var markIcon = ui.MustParseSVG(assets.Mark)

// panelTheme applies the brand theme to a desktop panel and keeps scroll
// indicators light while retaining native scrolling, keyboard navigation and
// pointer hit targets.
func panelTheme(c *ui.Context) {
	brand.Apply(c)
	theme := *c.Theme()
	theme.ScrollbarWidth = 3
	theme.Scrollbar = theme.Text.Alpha(.5)
	c.SetTheme(&theme)
}

// brandMark is the gradient tile with the product glyph.
func brandMark(c *ui.Context, size float32) {
	dark := c.Theme().Dark
	ui.Box(c).Size(size, size).Radius(size*.28).Shrink(0).Center().
		Gradient(brand.Accent(dark), brand.Violet(), 135).
		Children(func() {
			ui.Icon(c, markIcon).Size(size*.64, size*.64).TextColor(ui.Hex("#FFFFFF"))
		})
}

// panelHeader is the shared title row: brand mark, title, optional subtitle
// and trailing controls.
func panelHeader(c *ui.Context, title, subtitle string, trailing func()) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		brandMark(c, 30)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(18).FontWeight(650).MaxLines(1)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(1)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}
