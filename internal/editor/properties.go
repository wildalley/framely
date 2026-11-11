package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/doc"
)

const propertyGroupHeight = 44

func propertyCaption(c *ui.Context, caption string) {
	ui.Text(c, caption).FontSize(11).LineHeight(1).Height(14).TextColor(c.Theme().TextMuted)
}

// propertyNumber groups a typed value or a held stepper into one history
// change. Its text input keeps editing shortcuts away from the canvas.
func (a *App) propertyNumber(c *ui.Context, index int, key, label, caption string, value, lo, hi, width float64, set func(float64, bool)) {
	ui.Column(c.Key(key)).Width(float32(width)).Height(propertyGroupHeight).Shrink(0).Gap(2).Children(func() {
		propertyCaption(c, caption)
		ui.Row(c).Gap(0).Height(28).Radius(7).Border(1, c.Theme().Border).Background(c.Theme().Background).Clip().Children(func() {
			text := &a.propertyValues[index]
			if !a.propertyInputs[index].FocusWithin(c) {
				*text = strconv.FormatFloat(value, 'f', 0, 64)
			}
			field := ui.TextInput(c.Key("value"), text).Bind(&a.propertyInputs[index]).Label(label).Tooltip(label).
				Width(float32(width)-21).MinWidth(0).Height(28).Padding(2, 6).FontSize(12).Border(0, ui.Transparent)
			apply := func(v float64) {
				v = math.Round(clamp(v, lo, hi))
				if a.propertyEditKey != key {
					a.finishEditing()
					a.propertyEditKey = key
					a.history.Begin()
				}
				set(v, true)
			}
			if field.Changed() {
				if v, err := strconv.ParseFloat(strings.TrimSpace(*text), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi {
					apply(v)
				}
			}
			v := value
			if field.Shortcut(0, ui.KeyUp) {
				apply(value + 1)
				*text = strconv.FormatFloat(clamp(value+1, lo, hi), 'f', 0, 64)
			}
			if field.Shortcut(0, ui.KeyDown) {
				apply(value - 1)
				*text = strconv.FormatFloat(clamp(value-1, lo, hi), 'f', 0, 64)
			}
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border.Alpha(0.6))
			step := ui.Stepper(c.Key("step"), &v, lo, hi, 1).Label(label+"增减").Tooltip(label+"增减").
				Width(20).Height(28).Border(0, ui.Transparent)
			if step.Changed() {
				if step.Pressed() || step.Dragging() {
					apply(v)
				} else {
					a.finishEditing()
					set(v, false)
				}
				*text = strconv.FormatFloat(v, 'f', 0, 64)
			}
			if a.propertyEditKey == key && !field.Focused() && !step.Pressed() && !step.Dragging() {
				a.propertyEditKey = ""
				a.history.End()
			}
		})
	})
}

func (a *App) propertyColors(c *ui.Context, style doc.Style, expanded bool) {
	ui.Column(c).Height(propertyGroupHeight).Shrink(0).Gap(2).Children(func() {
		propertyCaption(c, "颜色")
		choose := func(col string) { a.finishEditing(); a.changeStyle(func(s *doc.Style) { s.Color = col }, false) }
		if expanded {
			ui.Row(c).Height(28).Gap(3).AlignItems(ui.Center).Children(func() {
				for _, col := range doc.Colors {
					b := ui.Button(c.Key("inline-color-"+col), "").Size(24, 24).Padding(0).Radius(12).
						Background(ui.Hex(col)).Border(1, c.Theme().Border).Label("颜色 " + col).Tooltip(col)
					if strings.EqualFold(style.Color, col) {
						b.Border(2.5, brand.Accent(c.Theme().Dark))
					}
					if b.Clicked() {
						choose(col)
					}
				}
			})
		} else {
			ui.Button(c.Key("inline-color"), "").Size(28, 28).Padding(0).Radius(6).
				Background(ui.Hex(style.Color)).Border(1, c.Theme().Border).Label("颜色").Tooltip("标注颜色").Menu(func(m *ui.Menu) {
				for i, col := range doc.Colors {
					names := []string{"红色", "橙色", "黄色", "绿色", "蓝色", "紫色", "黑色", "白色"}
					if m.Item(names[i]).Radio(strings.EqualFold(style.Color, col)).Chosen() {
						choose(col)
					}
				}
			})
		}
	})
}

// styleStrip floats over the top-left of the canvas while the
// current tool or the selected annotation has a style to change, and hides
// while drawing. The whole image's corners and the full set of options stay
// in the appearance popover.
func (a *App) styleStrip(c *ui.Context, stageWidth float32) {
	style := a.activeStyle()
	if style == nil || a.drag != nil {
		return
	}
	target := a.styleTarget()
	if target == doc.ToolMosaic {
		return
	}
	cellWidth := 64.0
	if stageWidth >= 520 {
		cellWidth = 80
	}
	ui.Row(c.Key("properties")).Label("快捷属性").Absolute().Top(10).Left(10).MaxWidth(stageWidth-20).
		Height(propertyGroupHeight+16).Padding(8, 12).Gap(8).AlignItems(ui.End).Radius(12).
		Background(c.Theme().Background).Border(1, c.Theme().Border).
		Shadow(0, 6, 18, 0, ui.RGBA(7, 61, 65, .14)).Disabled(a.busy).Children(func() {
		ui.Text(c, toolName(target)).FontSize(12).FontWeight(600).Height(28).MaxLines(1).Shrink(0)
		a.propertyColors(c, *style, stageWidth >= 560)
		index := 0
		if target == doc.ToolRectangle {
			a.propertyNumber(c, index, "inline-rect-corner", "矩形圆角", "圆角 %", style.CornerRatio*100, 0, 100, cellWidth, func(v float64, live bool) { a.changeStyle(func(s *doc.Style) { s.CornerRatio = v / 100 }, live) })
			index++
		}
		if target == doc.ToolText {
			a.propertyNumber(c, index, "inline-font", "字号", "字号 px", style.FontSize, 12, 96, cellWidth, func(v float64, live bool) { a.changeStyle(func(s *doc.Style) { s.FontSize = v }, live) })
		} else {
			hi := 16.0
			if target == doc.ToolHighlighter {
				hi = 40
			}
			a.propertyNumber(c, index, "inline-stroke", "粗细", "粗细 px", style.StrokeWidth, 1, hi, cellWidth, func(v float64, live bool) { a.changeStyle(func(s *doc.Style) { s.StrokeWidth = v }, live) })
		}
		index++
		a.propertyNumber(c, index, "inline-opacity", "不透明度", "不透明 %", style.Opacity*100, 10, 100, cellWidth, func(v float64, live bool) { a.changeStyle(func(s *doc.Style) { s.Opacity = v / 100 }, live) })
	})
}
