package editor

import (
	"fmt"
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
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

func commandKey() string {
	if runtime.GOOS == "darwin" {
		return "Cmd"
	}
	return "Ctrl"
}

// ShortcutRows is the image window's key reference, displayed from the tray.
func ShortcutRows() [][2]string {
	var rows [][2]string
	for _, it := range toolItems {
		rows = append(rows, [2]string{it.char, it.label})
	}
	mod := commandKey()
	return append(rows,
		[2]string{mod + "+Z", "撤销"},
		[2]string{mod + "+Shift+Z / " + mod + "+Y", "重做"},
		[2]string{mod + "+C", "复制图片"},
		[2]string{mod + "+V", "粘贴为新图片窗口"},
		[2]string{mod + "+S", "保存图片"},
		[2]string{mod + "++ / " + mod + "+-", "放大 / 缩小"},
		[2]string{mod + "+0", "100%"},
		[2]string{mod + "+滚轮 / 触控板捏合", "缩放"},
		[2]string{"Delete / Backspace", "删除所选标注"},
		[2]string{mod + "+W", "关闭并销毁图片"},
		[2]string{"Esc", "关闭样式 → 取消选择 → 关闭图片"},
		[2]string{"Tab / 方向键", "进入工具栏 / 切换控件"},
		[2]string{"双击文字 / " + mod + "+Enter", "编辑文字 / 完成编辑"},
	)
}

func (a *App) view(c *ui.Context) {
	if a.closed || a.image == nil {
		return
	}
	for _, message := range a.toasts {
		c.Toast(message)
	}
	a.toasts = nil
	a.shortcuts(c)
	if a.closed {
		return
	}
	if a.lastPanel != a.panel {
		a.lastPanel = a.panel
		if a.win != nil {
			a.win.Update(func() { a.accept(func() { a.fitWindow(false) }) })
		}
	}
	ww, wh := c.Size()
	z := math.Min(a.zoom, math.Min(float64(max(1, ww))/float64(a.image.Width),
		float64(max(1, wh-barHeight))/float64(a.image.Height)))
	a.displayZoom = z
	ui.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Clip().Children(func() {
			a.canvas(c, z)
		})
		a.bar(c)
	})
}

func (a *App) shortcuts(c *ui.Context) {
	if a.closed {
		return
	}
	// Closing stays available while encoding or waiting on a save dialog.
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.Close()
		return
	}
	if a.busy {
		return
	}
	typing := a.annotationText.FocusWithin(c) || a.styleText.FocusWithin(c)
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
		if c.Shortcut(ui.Cmd, ui.KeyV) && a.OnPaste != nil {
			a.OnPaste()
		}
	}
	if a.image != nil {
		if c.Shortcut(ui.Cmd, ui.KeyS) {
			a.Export()
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
		return
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
}

func (a *App) removeSelected() {
	a.finishEditing()
	list := a.history.Present()
	i := doc.IndexOf(list, a.selected)
	if i < 0 {
		return
	}
	a.history.Commit(append(append([]doc.Annotation{}, list[:i]...), list[i+1:]...))
	a.selected = ""
}

func (a *App) moveLayer(dir int) {
	a.finishEditing()
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

func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, disabled bool) ui.Element {
	return ui.Button(c.Key(key), "").Size(32, 32).Label(label).Tooltip(label).Disabled(disabled).
		Children(func() { ui.Icon(c, svg).Size(18, 18) })
}

func iconToggle(c *ui.Context, key string, svg *ui.SVG, label string, on *bool, disabled bool) ui.Element {
	return ui.Toggle(c.Key(key), on, "").Size(32, 32).Label(label).Tooltip(label).Disabled(disabled).
		Children(func() { ui.Icon(c, svg).Size(18, 18) })
}

// A single native tool row replaces the web editor's wrapped button grid.
// MyGo supplies focus navigation, selected states and an overflow menu. Image
// actions remain outside overflow, even on a narrow pinned image.
func (a *App) bar(c *ui.Context) {
	var styleAnchor ui.Element
	ui.Row(c.Key("bar")).FillWidth().Height(barHeight).Gap(4).Padding(4, 6).
		AlignItems(ui.Center).Background(c.Theme().Background).
		BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).DragWindow().Children(func() {
		ui.Toolbar(c.Key("tools"), func() {
			for _, it := range toolItems {
				on := a.tool == it.tool
				disabled := it.tool == doc.ToolMosaic && a.mosaic == nil
				if iconToggle(c, "tool-"+string(it.tool), it.icon, it.label+" ("+it.char+")", &on, disabled).Changed() {
					a.setTool(it.tool)
				}
			}
			if iconButton(c, "undo", iconUndo, "撤销 ("+commandKey()+"+Z)", !a.history.CanUndo()).Clicked() {
				a.undo()
			}
			if iconButton(c, "redo", iconRedo, "重做 ("+commandKey()+"+Shift+Z)", !a.history.CanRedo()).Clicked() {
				a.redo()
			}
		}).Label("标注工具").Grow(1).MinWidth(0).Padding(0).Disabled(a.busy)
		styleOpen := a.panel == PanelStyle
		styleAnchor = iconToggle(c, "style", iconPalette, "样式", &styleOpen, a.activeStyle() == nil || a.busy)
		if styleAnchor.Changed() {
			a.togglePanel(PanelStyle)
		}
		pinned := a.pinned
		if iconToggle(c, "pin", iconPin, "置顶", &pinned, false).Changed() {
			a.TogglePin()
		}
		iconButton(c, "save", iconDownload, "保存图片 ("+commandKey()+"+S)", a.busy).Menu(a.imageMenu)
		copy := ui.PrimaryButton(c.Key("copy"), "复制").Height(32).Padding(0, 10).Disabled(a.busy).
			Tooltip("复制图片 (" + commandKey() + "+C)")
		if copy.Clicked() {
			a.Copy()
		}
		if iconButton(c, "close", iconClose, "关闭图片", false).Clicked() {
			a.Close()
		}
	})
	if a.closed {
		return
	}
	style := a.activeStyle()
	if a.panel == PanelStyle && style != nil {
		open := true
		panel := ui.Popover(c.Key("style-popover"), styleAnchor, &open, func() {
			ui.Scroll(c).Width(styleWidth).MaxHeight(styleHeight - 20).Padding(8).Gap(12).
				Children(func() { a.stylePanel(c, *style) })
		})
		if panel.Dismissed() || !open {
			a.finishEditing()
			a.panel = PanelNone
		}
	} else if a.panel == PanelStyle {
		a.finishEditing()
		a.panel = PanelNone
	}
}

func (a *App) imageMenu(m *ui.Menu) {
	for _, format := range render.Formats {
		label := "保存 " + format.Label()
		if format.Lossy() {
			label = fmt.Sprintf("保存 JPEG（%d%%）", int(math.Round(a.exportQuality*100)))
		}
		if m.Item(label).Disabled(a.busy).Chosen() {
			a.exportFormat = format
			a.Export()
		}
	}
	m.Submenu("JPEG 质量", func(m *ui.Menu) {
		for _, quality := range []int{60, 80, 92, 100} {
			if m.Item(fmt.Sprintf("%d%%", quality)).Radio(int(math.Round(a.exportQuality*100)) == quality).Chosen() {
				a.exportQuality = float64(quality) / 100
			}
		}
	})
	m.Separator()
	m.Submenu(fmt.Sprintf("缩放（%d%%）", int(math.Round(a.displayZoom*100))), func(m *ui.Menu) {
		if m.Item("放大").Chosen() {
			a.setZoom(a.displayZoom + 0.1)
		}
		if m.Item("缩小").Chosen() {
			a.setZoom(a.displayZoom - 0.1)
		}
		if m.Item("100%").Chosen() {
			a.setZoom(1)
		}
		if m.Item("适应屏幕").Chosen() {
			a.zoomToScreen()
		}
	})
	m.Item(fmt.Sprintf("%d × %d px", a.image.Width, a.image.Height)).Disabled(true)
}

func (a *App) contextMenu(m *ui.Menu) {
	if m.Item("复制图片").Disabled(a.busy).Chosen() {
		a.Copy()
	}
	if m.Item("保存图片").Disabled(a.busy).Chosen() {
		a.Export()
	}
	if m.Item("置顶").Checked(a.pinned).Chosen() {
		a.TogglePin()
	}
	m.Separator()
	if m.Item("撤销").Disabled(!a.history.CanUndo() || a.busy).Chosen() {
		a.undo()
	}
	if m.Item("重做").Disabled(!a.history.CanRedo() || a.busy).Chosen() {
		a.redo()
	}
	if a.selected != "" {
		if m.Item("标注样式").Disabled(a.busy).Chosen() {
			a.togglePanel(PanelStyle)
		}
		if m.Item("上移一层").Disabled(a.busy).Chosen() {
			a.moveLayer(1)
		}
		if m.Item("下移一层").Disabled(a.busy).Chosen() {
			a.moveLayer(-1)
		}
		if m.Item("删除标注").Disabled(a.busy).Chosen() {
			a.removeSelected()
		}
	}
	m.Separator()
	if m.Item("关闭图片").Chosen() {
		a.Close()
	}
}

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
