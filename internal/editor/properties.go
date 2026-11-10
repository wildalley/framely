package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/brand"
	"github.com/wildalley/screenpin/internal/doc"
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

func (a *App) propertyRow(c *ui.Context, width float32) {
	ui.Row(c.Key("properties")).Label("快捷属性").FillWidth().Height(propertyHeight).Gap(6).Padding(4, 8).AlignItems(ui.End).
		Background(c.Theme().Background).Disabled(a.busy).Children(func() {
		style := a.activeStyle()
		scope := "截图"
		options := []string{"截图"}
		if style != nil {
			options = []string{toolName(a.styleTarget()), "截图"}
			if !a.propertyImage {
				scope = options[0]
			}
		}
		scopeWidth := float32(64)
		if len([]rune(scope)) > 2 {
			scopeWidth = 80
		}
		ui.Column(c).Width(scopeWidth).Height(propertyGroupHeight).Shrink(0).Gap(2).Children(func() {
			propertyCaption(c, "目标")
			if ui.Select(c.Key("property-scope"), &scope, options).Label("属性目标").FillWidth().MinWidth(0).Height(28).FontSize(12).Padding(2, 4).Radius(7).Changed() {
				a.finishEditing()
				a.propertyImage = scope == "截图"
			}
		})
		cellWidth := 64.0
		if width >= 520 {
			cellWidth = 80
		}
		if scope == "截图" {
			a.propertyNumber(c, 0, "inline-image-corner", "截图圆角", "圆角 %", a.history.ImageCornerRatio()*100, 0, 100, cellWidth, func(v float64, _ bool) { a.history.SetImageCornerRatio(v / 100) })
			{
				presets := []int{0, 50, 100}
				if width >= 520 {
					presets = []int{0, 10, 25, 50, 75, 100}
				}
				ui.Column(c).Height(propertyGroupHeight).Shrink(0).Gap(2).Children(func() {
					propertyCaption(c, "预设")
					// A segmented control: the chosen preset is a raised white tab.
					ui.Row(c).Height(28).AlignItems(ui.Center).Gap(1).Radius(8).Background(c.Theme().Surface).Padding(2).Children(func() {
						for _, v := range presets {
							on := math.Abs(a.history.ImageCornerRatio()*100-float64(v)) < .5
							toggle := ui.Toggle(c.Key(fmt.Sprintf("inline-image-%d", v)), &on, fmt.Sprintf("%d%%", v)).
								Label(fmt.Sprintf("快捷圆角 %d%%", v)).Height(24).Padding(0, 6).Radius(6).FontSize(11).
								Border(0, ui.Transparent)
							if on {
								toggle.Background(c.Theme().Background).TextColor(brand.Ink(c)).FontWeight(600).
									Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .12))
							} else {
								toggle.Background(ui.Transparent).TextColor(c.Theme().TextMuted)
							}
							if toggle.Changed() {
								a.finishEditing()
								a.history.SetImageCornerRatio(float64(v) / 100)
							}
						}
					})
				})
			}
		} else {
			target := a.styleTarget()
			if target != doc.ToolMosaic {
				a.propertyColors(c, *style, width >= 900)
			}
			index := 0
			if target == doc.ToolRectangle {
				a.propertyNumber(c, index, "inline-rect-corner", "矩形圆角", "圆角 %", style.CornerRatio*100, 0, 100, cellWidth, func(v float64, live bool) { a.changeStyle(func(s *doc.Style) { s.CornerRatio = v / 100 }, live) })
				index++
			}
			if target != doc.ToolMosaic {
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
				index++
			} else {
				ui.Column(c).Height(propertyGroupHeight).Shrink(0).Gap(2).Children(func() {
					propertyCaption(c, "说明")
					ui.Row(c).Height(28).AlignItems(ui.Center).Children(func() { ui.Text(c, "保持不透明").FontSize(12).TextColor(c.Theme().TextMuted) })
				})
			}
			if width >= 600 || target == doc.ToolMosaic {
				a.propertyNumber(c, index, "inline-image-corner", "截图圆角", "截图圆角 %", a.history.ImageCornerRatio()*100, 0, 100, cellWidth, func(v float64, _ bool) { a.history.SetImageCornerRatio(v / 100) })
			}
		}
	})
}
