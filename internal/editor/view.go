package editor

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
)

// Colors of the editor, as the web editor had them.
var (
	colSurface = ui.Hex("#fafaf8")
	colInk     = ui.Hex("#202326")
	colBarInk  = ui.Hex("#303638")
	colMuted   = ui.Hex("#737b80")
	colLine    = ui.Hex("#c9cdcd")
	colHover   = ui.Hex("#e9ebea")
	colAccent  = ui.Hex("#157f5b")
	colAccentD = ui.Hex("#0d6849")
	colSoft    = ui.Hex("#e2f2eb")
	colStage   = ui.Hex("#1c2021")
	colDanger  = ui.Hex("#c4343a")
)

type toolItem struct {
	tool  doc.Tool
	label string
	icon  *ui.SVG
	key   ui.Key
	char  string
}

var toolItems = []toolItem{
	{doc.ToolSelect, "选择", iconSelect, ui.KeyV, "V"},
	{doc.ToolRectangle, "矩形", iconRect, ui.KeyR, "R"},
	{doc.ToolEllipse, "椭圆", iconEllipse, ui.KeyO, "O"},
	{doc.ToolArrow, "箭头", iconArrow, ui.KeyA, "A"},
	{doc.ToolPen, "画笔", iconPen, ui.KeyP, "P"},
	{doc.ToolHighlighter, "荧光笔", iconHighlighter, ui.KeyH, "H"},
	{doc.ToolAreaHighlight, "区域高亮", iconArea, ui.KeyG, "G"},
	{doc.ToolText, "文字", iconText, ui.KeyT, "T"},
	{doc.ToolMosaic, "马赛克", iconMosaic, ui.KeyM, "M"},
}

func toolName(t doc.Tool) string {
	for _, it := range toolItems {
		if it.tool == t {
			return it.label
		}
	}
	return string(t)
}

// The bar's metrics. Its items have fixed widths, so the editor knows how
// many rows the bar takes at a width before it lays it out, and sizes the
// window for them in one step.
const (
	barButton   = 30
	barGap      = 2
	barDivider  = 11 // a divider and the room around it
	barCopy     = 70
	barPadX     = 8
	barPadY     = 6
	barRow      = 34
	panelMargin = 16
)

// barSlots are the widths of the bar's items, in order.
func barSlots() []int {
	var slots []int
	for range toolItems {
		slots = append(slots, barButton)
	}
	slots = append(slots, barDivider, barButton, barButton, barButton, barDivider)
	slots = append(slots, barButton, barButton, barButton, barButton, barButton, barCopy, barButton)
	return slots
}

// barRows splits the bar's items into the rows they wrap to in width.
func barRows(width int) [][2]int {
	slots := barSlots()
	avail := max(barButton, width-2*barPadX)
	var rows [][2]int
	start, used := 0, 0
	for i, w := range slots {
		need := w
		if i > start {
			need += barGap
		}
		if i > start && used+need > avail {
			rows = append(rows, [2]int{start, i})
			start, used = i, w
			continue
		}
		used += need
	}
	return append(rows, [2]int{start, len(slots)})
}

func barHeightFor(width int) int {
	return len(barRows(width))*barRow + 2*barPadY + 1
}

func panelWidth(p Panel) int {
	switch p {
	case PanelShortcuts:
		return 340
	case PanelSettings:
		return 420
	}
	return 248
}

func (a *App) view(c *ui.Context) {
	for _, msg := range a.toasts {
		c.Toast(msg)
	}
	a.toasts = nil
	a.shortcuts(c)
	if a.image == nil {
		a.emptyState(c)
		return
	}
	if a.lastPanel != a.panel {
		a.lastPanel = a.panel
		a.panelHeight = 0
		a.fitWindow(false)
	}
	ww, wh := c.Size()
	barH := float32(barHeightFor(int(ww)))
	// The window manager may give the window another size than asked, as
	// a tiling one does: fit the image to what the window got.
	z := math.Min(a.zoom, math.Min(float64(max(1, ww))/float64(a.image.Width),
		float64(max(1, wh-barH))/float64(a.image.Height)))
	a.displayZoom = z
	ui.Column(c).Fill().Background(colStage).Children(func() {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Start).Clip().Children(func() {
			a.canvas(c, z)
		})
		a.bar(c)
		a.panels(c, barH)
	})
}

// shortcuts are the editor's keys, which a focused text field leaves alone.
func (a *App) shortcuts(c *ui.Context) {
	if a.settings.recording || a.busy || a.quitPending || a.quitting {
		return
	}
	typing := a.annotationText.FocusWithin(c) || a.styleText.FocusWithin(c) || a.projectTitle.FocusWithin(c)
	if !typing {
		if c.Shortcut(ui.Cmd, ui.KeyZ) {
			a.undo()
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) || c.Shortcut(ui.Cmd, ui.KeyY) {
			a.redo()
		}
		if a.image != nil && c.Shortcut(ui.Cmd, ui.KeyC) {
			a.Copy()
		}
		if c.Shortcut(ui.Cmd, ui.KeyV) {
			a.Paste()
		}
	}
	if a.image != nil {
		if c.Shortcut(ui.Cmd, ui.KeyS) {
			a.Export()
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
			a.SaveProject()
		}
		if c.Shortcut(ui.Cmd, ui.KeyEqual) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual) {
			a.setZoom(a.displayZoom + 0.1)
		}
		if c.Shortcut(ui.Cmd, ui.KeyMinus) {
			a.setZoom(a.displayZoom - 0.1)
		}
		if c.Shortcut(ui.Cmd, ui.Key0) {
			a.setZoom(1)
		}
	}
	if typing {
		return
	}
	if c.Shortcut(0, ui.KeyDelete) || c.Shortcut(0, ui.KeyBackspace) {
		a.removeSelected()
	}
	if c.Shortcut(0, ui.KeyEscape) {
		// Peel back one layer a press: the panel, then the selection or
		// the tool, then the window itself.
		switch {
		case a.panel != PanelNone:
			a.finishEditing()
			a.panel = PanelNone
		case a.selected != "" || a.tool != doc.ToolSelect:
			a.selected = ""
			a.tool = doc.ToolSelect
		default:
			a.Close()
		}
	}
	if c.Shortcut(ui.Shift, ui.KeySlash) || c.Shortcut(0, ui.KeySlash) {
		a.togglePanel(PanelShortcuts)
	}
	if a.image == nil {
		return
	}
	for _, it := range toolItems {
		if c.Shortcut(0, it.key) {
			if it.tool == doc.ToolMosaic && a.mosaic == nil {
				c.Toast("马赛克当前不可用")
				continue
			}
			a.setTool(it.tool)
		}
	}
}

func (a *App) undo() {
	a.finishEditing()
	a.history.Undo()
	if doc.IndexOf(a.history.Present(), a.selected) < 0 {
		a.selected = ""
	}
}

func (a *App) redo() {
	a.finishEditing()
	a.history.Redo()
	if doc.IndexOf(a.history.Present(), a.selected) < 0 {
		a.selected = ""
	}
}

func (a *App) setTool(t doc.Tool) {
	a.finishEditing()
	a.tool = t
	// A selection left from before would make the style panel restyle it
	// instead of setting up the new tool.
	if t != doc.ToolSelect {
		a.selected = ""
	}
	if t == doc.ToolSelect && a.panel == PanelStyle {
		a.panel = PanelNone
	}
	a.hoverCursor = ui.CursorDefault
}

func (a *App) togglePanel(p Panel) {
	a.finishEditing()
	if a.panel == p {
		a.panel = PanelNone
	} else {
		a.panel = p
	}
	if a.panel == PanelSettings {
		a.openSettings()
	}
}

func (a *App) removeSelected() {
	list := a.history.Present()
	i := doc.IndexOf(list, a.selected)
	if i < 0 {
		return
	}
	a.history.Commit(append(append([]doc.Annotation{}, list[:i]...), list[i+1:]...))
	a.selected = ""
}

func (a *App) moveLayer(dir int) {
	list := a.history.Present()
	i := doc.IndexOf(list, a.selected)
	j := i + dir
	if i < 0 || j < 0 || j >= len(list) {
		return
	}
	next := append([]doc.Annotation{}, list...)
	next[i], next[j] = next[j], next[i]
	a.history.Commit(next)
}

// iconButton is a square button of the bar.
func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, active, disabled bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(barButton, barButton).Radius(5).Center().
		TextColor(colBarInk).Tooltip(label).Label(label).Disabled(disabled)
	if active {
		b.Background(colSoft).TextColor(colAccentD)
	} else if b.Hovered() && !disabled {
		b.Background(colHover)
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(17) })
	return b
}

func divider(c *ui.Context) {
	ui.Box(c).Size(1, 20).Background(colLine).Margin(0, (barDivider-1-2*barGap)/2)
}

func (a *App) bar(c *ui.Context) {
	ww, _ := c.Size()
	rows := barRows(int(ww))
	style := a.activeStyle()
	items := a.barItems(c, style)
	bar := ui.Column(c.Key("bar")).FillWidth().Padding(barPadY, barPadX).Background(colSurface).Disabled(a.busy).
		BorderWidth(1, 0, 0, 0).BorderColor(colLine).DragWindow().Cursor(ui.CursorGrab)
	bar.Children(func() {
		for _, r := range rows {
			ui.Row(c).Height(barRow).Gap(barGap).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				for _, item := range items[r[0]:r[1]] {
					item()
				}
			})
		}
	})
}

// barItems builds each item of the bar, in the order of barSlots.
func (a *App) barItems(c *ui.Context, style *doc.Style) []func() {
	var items []func()
	for _, it := range toolItems {
		items = append(items, func() {
			disabled := it.tool == doc.ToolMosaic && a.mosaic == nil
			label := it.label + " (" + it.char + ")"
			if disabled {
				label = "马赛克处理失败"
			}
			if iconButton(c, "tool-"+string(it.tool), it.icon, label, a.tool == it.tool, disabled).Clicked() {
				a.setTool(it.tool)
			}
		})
	}
	items = append(items,
		func() { divider(c) },
		func() {
			label := "样式"
			if style == nil {
				label = "选中标注或先选择工具"
			}
			b := iconButton(c, "style", iconPalette, label, a.panel == PanelStyle, style == nil)
			if style != nil {
				color := style.Color
				b.Children(func() {
					ui.Box(c).Absolute().Right(4).Bottom(4).Size(8, 8).Radius(4).
						Background(ui.Hex(color)).Border(1, ui.RGBA(0, 0, 0, 0.2))
				})
			}
			if b.Clicked() {
				a.togglePanel(PanelStyle)
			}
		},
		func() {
			if iconButton(c, "undo", iconUndo, "撤销", false, !a.history.CanUndo()).Clicked() {
				a.undo()
			}
		},
		func() {
			if iconButton(c, "redo", iconRedo, "重做", false, !a.history.CanRedo()).Clicked() {
				a.redo()
			}
		},
		func() { divider(c) },
		func() {
			if iconButton(c, "help", iconHelp, "快捷键 (?)", a.panel == PanelShortcuts, false).Clicked() {
				a.togglePanel(PanelShortcuts)
			}
		},
		func() {
			if iconButton(c, "settings", iconSettings, "设置", a.panel == PanelSettings, false).Clicked() {
				a.togglePanel(PanelSettings)
			}
		},
		func() {
			svg, label := iconPin, "置顶"
			if a.pinned {
				svg, label = iconPinOff, "取消置顶"
			}
			if iconButton(c, "pin", svg, label, a.pinned, false).Clicked() {
				a.TogglePin()
			}
		},
		func() {
			if iconButton(c, "export", iconDownload, "导出 "+a.exportFormat.Label()+" (Ctrl+S)", false, false).Clicked() {
				a.Export()
			}
		},
		func() {
			if iconButton(c, "more", iconMore, "更多", a.panel == PanelMenu, false).Clicked() {
				a.togglePanel(PanelMenu)
			}
		},
		func() {
			b := ui.ButtonBase(c.Key("copy")).Size(barCopy, barButton).Radius(5).Gap(5).Center().
				Background(colAccent).TextColor(ui.RGBA(255, 255, 255, 1)).Label("复制")
			if b.Hovered() {
				b.Background(colAccentD)
			}
			b.Children(func() {
				ui.Icon(c, iconCopy).FontSize(15)
				ui.Text(c, "复制").FontSize(13).FontWeight(600).SingleLine()
			})
			if b.Clicked() {
				a.Copy()
			}
		},
		func() {
			if iconButton(c, "close", iconClose, a.closeLabel(), false, false).Clicked() {
				a.Close()
			}
		},
	)
	return items
}

// activeStyle is what the style panel edits: the selection, or else the
// active tool's look for what it draws next.
func (a *App) activeStyle() *doc.Style {
	if sel := a.selectedAnnotation(); sel != nil {
		s := sel.Style()
		return &s
	}
	if a.tool == doc.ToolSelect {
		return nil
	}
	s := a.styles[a.tool]
	return &s
}

func (a *App) styleTarget() doc.Tool {
	if sel := a.selectedAnnotation(); sel != nil {
		return sel.Type
	}
	return a.tool
}

func (a *App) emptyState(c *ui.Context) {
	ui.Column(c.Key("empty")).Fill().Center().Gap(16).Padding(22).Background(colSurface).
		DragWindow().Cursor(ui.CursorGrab).Children(func() {
		ui.Box(c).Absolute().Top(8).Right(8).Children(func() {
			if iconButton(c, "empty-close", iconClose, a.closeLabel(), false, false).Clicked() {
				a.Close()
			}
		})
		ui.Box(c).Size(50, 50).Radius(8).Background(ui.Hex("#252b2b")).Center().
			TextColor(ui.RGBA(255, 255, 255, 1)).Children(func() {
			ui.Icon(c, iconCrop).FontSize(24)
		})
		ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "ScreenPin").FontSize(20).FontWeight(700).TextColor(colInk)
			ui.Text(c, "截图后直接标注、置顶和复制").FontSize(13).TextColor(colMuted)
		})
		ui.Row(c).Gap(7).Wrap().Justify(ui.Center).Children(func() {
			b := primaryAction(c, "empty-capture", iconCapture, "截取屏幕").Disabled(a.busy)
			if b.Clicked() {
				a.Capture()
			}
			if secondaryAction(c, "empty-open", iconImage, "打开图片").Disabled(a.busy).Clicked() {
				a.OpenImage()
			}
			if secondaryAction(c, "empty-project", iconFolder, "打开工程").Disabled(a.busy).Clicked() {
				a.OpenProject()
			}
		})
		ui.Text(c, "全局快捷键 "+a.ShortcutDescription()).FontSize(12).TextColor(colMuted)
	})
}

func primaryAction(c *ui.Context, key string, svg *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(36).Padding(0, 14).Radius(6).Gap(6).Center().
		Background(colAccent).TextColor(ui.RGBA(255, 255, 255, 1))
	if b.Hovered() {
		b.Background(colAccentD)
	}
	b.Children(func() {
		ui.Icon(c, svg).FontSize(16)
		ui.Text(c, label).FontSize(14).FontWeight(600).SingleLine()
	})
	return b
}

func secondaryAction(c *ui.Context, key string, svg *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(36).Padding(0, 14).Radius(6).Gap(6).Center().
		Background(ui.RGBA(255, 255, 255, 1)).Border(1, colLine).TextColor(colInk)
	if b.Hovered() {
		b.Background(colHover)
	}
	b.Children(func() {
		ui.Icon(c, svg).FontSize(16)
		ui.Text(c, label).FontSize(14).SingleLine()
	})
	return b
}
