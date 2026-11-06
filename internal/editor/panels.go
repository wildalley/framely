package editor

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
)

func panelTitle(c *ui.Context, title, hint string) {
	ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontWeight(600)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
}

// changeStyle changes the look of the selection, and the preset of its
// tool, or else of the active tool. live changes hold the slider's
// transaction instead of making a step each.
func (a *App) changeStyle(change func(*doc.Style), live bool) {
	t := a.styleTarget()
	if t == doc.ToolSelect {
		return
	}
	preset := a.styles[t]
	change(&preset)
	a.styles[t] = preset
	list := a.history.Present()
	i := doc.IndexOf(list, a.selected)
	if i < 0 {
		return
	}
	an := list[i].Clone()
	s := an.Style()
	change(&s)
	an.SetStyle(s)
	next := doc.Replace(list, i, an)
	if live {
		a.history.SetTransient(next)
	} else {
		a.history.Commit(next)
	}
}

// slider is a labeled slider whose drag is one step of the history.
func (a *App) slider(c *ui.Context, key, label string, value, lo, hi float64, suffix string, set func(v float64, live bool)) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, label).TextColor(c.Theme().TextMuted).FontSize(12)
			ui.Textf(c, "%d%s", int(math.Round(value)), suffix).Font("monospace").FontSize(12)
		})
		v := value
		s := ui.StepSlider(c.Key(key), &v, lo, hi, 1).Label(label).FillWidth()
		held := s.Pressed() || s.Dragging()
		if s.Changed() {
			if held {
				if a.slidingKey == "" {
					a.slidingKey = key
					a.history.Begin()
				}
				set(v, true)
			} else {
				set(v, false)
			}
		}
		if a.slidingKey == key && !held {
			a.slidingKey = ""
			a.history.End()
		}
	})
}

func (a *App) stylePanel(c *ui.Context, style doc.Style) {
	target := a.styleTarget()
	sel := a.selectedAnnotation()
	hint := "下次绘制"
	if sel != nil {
		hint = "编辑所选"
	}
	panelTitle(c, toolName(target), hint)
	if target != doc.ToolMosaic {
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, col := range doc.Colors {
				b := ui.Button(c.Key("color-"+col), "").Size(28, 28).Radius(6).
					Background(ui.Hex(col)).Border(1, ui.RGBA(0, 0, 0, 0.18)).Label("颜色 " + col).Tooltip(col)
				if strings.EqualFold(style.Color, col) {
					b.Border(2, c.Theme().Accent)
				}
				if b.Clicked() {
					a.changeStyle(func(s *doc.Style) { s.Color = col }, false)
				}
			}
		})
	}
	if target != doc.ToolMosaic && target != doc.ToolText {
		hi := 16.0
		if target == doc.ToolHighlighter {
			hi = 40
		}
		a.slider(c, "stroke", "粗细", style.StrokeWidth, 1, hi, "px", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.StrokeWidth = v }, live)
		})
	}
	if target == doc.ToolText {
		a.slider(c, "font", "字号", style.FontSize, 12, 96, "px", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.FontSize = v }, live)
		})
	}
	if target != doc.ToolMosaic {
		a.slider(c, "opacity", "不透明度", style.Opacity*100, 10, 100, "%", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.Opacity = v / 100 }, live)
		})
	}
	if sel != nil && sel.Type == doc.ToolText && a.editingID == "" {
		ui.Text(c, "文字").FontSize(12).TextColor(c.Theme().TextMuted)
		text := sel.TextValue()
		field := ui.TextArea(c.Key("style-text"), &text).Bind(&a.styleText).Label("标注文字").Lines(2, 6).FillWidth()
		if field.Changed() {
			if !a.textEdit {
				a.textEdit = true
				a.history.Begin()
			}
			list := a.history.Present()
			if i := doc.IndexOf(list, sel.ID); i >= 0 {
				an := list[i].Clone()
				an.SetText(text)
				a.history.SetTransient(doc.Replace(list, i, an))
			}
		}
		if a.textEdit && !field.Focused() {
			a.textEdit = false
			a.history.End()
		}
	}
	if sel != nil {
		ui.Row(c).Gap(6).Children(func() {
			if iconButton(c, "layer-down", iconDown, "下移一层", false).Clicked() {
				a.moveLayer(-1)
			}
			if iconButton(c, "layer-up", iconUp, "上移一层", false).Clicked() {
				a.moveLayer(1)
			}
			if iconButton(c, "delete", iconTrash, "删除 (Delete)", false).TextColor(c.Theme().Danger).Clicked() {
				a.removeSelected()
			}
		})
	}
}
