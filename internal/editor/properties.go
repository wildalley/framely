package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/doc"
)

// styleFlyoutWidth is the panel that opens beside the tool rail.
const styleFlyoutWidth = 184

var colorNames = map[string]string{
	"#e5484d": "红色", "#ff8a3d": "橙色", "#f2c94c": "黄色", "#1f9d73": "绿色",
	"#2383e2": "蓝色", "#7c5cff": "紫色", "#171717": "黑色", "#ffffff": "白色",
}

// preset is one choice of a segmented preset row: its glyph is drawn by
// glyph, sized for the value.
type preset struct {
	label string
	value float64
}

// strokePresets are thin, medium and thick for the tool.
func strokePresets(t doc.Tool) []preset {
	if t == doc.ToolHighlighter {
		return []preset{{"细", 12}, {"中", 18}, {"粗", 30}}
	}
	return []preset{{"细", 2}, {"中", 4}, {"粗", 8}}
}

var fontPresets = []preset{{"小", 18}, {"中", 28}, {"大", 44}}

var cornerPresets = []preset{{"直角", 0}, {"小圆角", .1}, {"大圆角", .3}}

// styleFlyout opens beside the tool rail while the current tool or the
// selected annotation has a style: color dots, thin/medium/thick, corner
// presets and an opacity slider. Exact numbers stay in the full style
// popover. It hides while drawing; clicking the active tool again or Esc
// puts it away.
func (a *App) styleFlyout(c *ui.Context, stageWidth, stageHeight float32, zoom float64) {
	style := a.activeStyle()
	if style == nil || a.drag != nil || a.styleHidden {
		return
	}
	target := a.styleTarget()
	if target == doc.ToolMosaic {
		return
	}
	t := c.Theme()
	panel := ui.Scroll(c.Key("properties")).Label("快捷属性").Absolute().Top(8).Width(styleFlyoutWidth).
		MaxHeight(stageHeight-16).Padding(12).Gap(10).Radius(12).
		Background(t.Background).Border(1, t.Border).Shadow(0, 8, 22, 0, ui.RGBA(7, 61, 65, .14)).
		Disabled(a.busy)
	if a.selectionUnderFlyout(stageWidth, stageHeight, zoom) {
		panel.Right(8)
	} else {
		panel.Left(8)
	}
	panel.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Margin(0, 0, -6).Children(func() {
			ui.Text(c, "颜色").FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Spacer(c).Grow(1)
			more := ui.Button(c.Key("more-style"), "更多…").Height(18).Padding(0, 2).FontSize(11).
				Background(ui.Transparent).Border(0, ui.Transparent).TextColor(brand.Ink(c)).
				Label("更多样式").Tooltip("全部样式与精确数值")
			if more.Clicked() {
				a.appearanceTab = 0
				a.togglePanel(PanelStyle)
			}
		})
		ui.Row(c.Key("colors")).FillWidth().Wrap().Gap(8).Children(func() {
			for _, col := range doc.Colors {
				a.colorDot(c, col, strings.EqualFold(style.Color, col))
			}
		})
		if target == doc.ToolText {
			flyoutCaption(c, "字号", fmt.Sprintf("%.0f", style.FontSize))
			a.presetRow(c, "font", fontPresets, style.FontSize, func(v float64) float32 { return float32(v) / 4 }, glyphText,
				func(v float64) { a.changeStyle(func(s *doc.Style) { s.FontSize = v }, false) })
		} else {
			flyoutCaption(c, "粗细", fmt.Sprintf("%.0f", style.StrokeWidth))
			scale := float32(1)
			if target == doc.ToolHighlighter {
				scale = .25
			}
			a.presetRow(c, "stroke", strokePresets(target), style.StrokeWidth, func(v float64) float32 { return float32(v) * scale }, glyphLine,
				func(v float64) { a.changeStyle(func(s *doc.Style) { s.StrokeWidth = v }, false) })
		}
		if target == doc.ToolRectangle {
			flyoutCaption(c, "边角", "")
			a.presetRow(c, "corner", cornerPresets, style.CornerRatio, func(v float64) float32 { return float32(v) }, glyphCorner,
				func(v float64) { a.changeStyle(func(s *doc.Style) { s.CornerRatio = v }, false) })
		}
		a.slider(c, "inline-opacity", "不透明度", style.Opacity*100, 10, 100, "%", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.Opacity = v / 100 }, live)
		})
	})
}

// selectionUnderFlyout reports whether the selected annotation would sit
// under the flyout at the stage's left while the right side has room, so
// the flyout moves there rather than hide what is being styled.
func (a *App) selectionUnderFlyout(stageWidth, stageHeight float32, zoom float64) bool {
	sel := a.selectedAnnotation()
	if sel == nil || a.image == nil {
		return false
	}
	b := sel.Bounds()
	left := (float64(stageWidth) - float64(a.image.Width)*zoom) / 2
	start, end := left+b.X*zoom, left+(b.X+b.W)*zoom
	reach := float64(8 + styleFlyoutWidth + 8)
	return start < reach && float64(stageWidth)-end >= reach
}

func flyoutCaption(c *ui.Context, caption, value string) {
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Margin(0, 0, -6).Children(func() {
		ui.Text(c, caption).FontSize(11).TextColor(c.Theme().TextMuted)
		if value != "" {
			ui.Text(c, value).FontSize(11).Font("monospace").TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *App) colorDot(c *ui.Context, col string, chosen bool) {
	t := c.Theme()
	name := colorNames[col]
	if name == "" {
		name = col
	}
	b := ui.Button(c.Key("color-"+col), "").Size(30, 30).Padding(0).Radius(15).Center().
		Background(ui.Transparent).Border(0, ui.Transparent).Label("颜色 " + name).Tooltip(name)
	if chosen {
		b.Border(2, brand.Ink(c))
	}
	b.Children(func() {
		edge := t.Text.Alpha(.15)
		if t.Dark {
			edge = t.Text.Alpha(.35) // Keeps the black dot visible on a dark panel.
		}
		ui.Box(c).Size(22, 22).Radius(11).Background(ui.Hex(col)).Border(1, edge)
	})
	if b.Clicked() {
		a.finishEditing()
		a.changeStyle(func(s *doc.Style) { s.Color = col }, false)
	}
}

type glyph int

const (
	glyphLine glyph = iota
	glyphCorner
	glyphText
)

// presetRow is a segmented control; the chosen preset is a raised tab. A
// value set elsewhere to something in between chooses none.
func (a *App) presetRow(c *ui.Context, key string, presets []preset, value float64, size func(float64) float32, kind glyph, set func(float64)) {
	t := c.Theme()
	ui.Row(c.Key(key)).FillWidth().Height(30).Gap(2).Padding(2).Radius(8).Background(t.Surface).Children(func() {
		for _, p := range presets {
			on := math.Abs(value-p.value) < 1e-3
			name := map[string]string{"stroke": "粗细", "font": "字号", "corner": "边角"}[key]
			b := ui.Toggle(c.Key(fmt.Sprintf("%s-%v", key, p.value)), &on, "").Grow(1).Height(26).Padding(0).Radius(6).Center().
				Border(0, ui.Transparent).Label(name + " " + p.label).Tooltip(p.label)
			if on && t.Dark {
				b.Background(t.SurfacePressed)
			} else if on {
				b.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .12))
			} else {
				b.Background(ui.Transparent)
			}
			ink := t.Text
			if on {
				ink = brand.Ink(c)
			}
			v := p.value
			b.Children(func() {
				switch kind {
				case glyphLine:
					h := max(1.5, min(9, size(v)))
					ui.Box(c).Size(18, h).Radius(h / 2).Background(ink)
				case glyphCorner:
					r := float32(0)
					if v > 0 {
						r = min(6, 2.5+size(v)*12)
					}
					ui.Box(c).Size(16, 13).Radius(r).Border(2, ink)
				case glyphText:
					ui.Text(c, "A").FontSize(max(10, min(18, size(v)*1.6))).FontWeight(600).TextColor(ink)
				}
			})
			if b.Changed() {
				a.finishEditing()
				set(v)
			}
		}
	})
}
