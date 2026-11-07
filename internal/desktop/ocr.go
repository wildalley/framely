package desktop

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/ocr"
)

func (s *application) showOCR(result ocr.Result) {
	if s.scroll != nil && s.scroll.running {
		s.scroll.show()
	}
	text := result.Text()
	tab := 0
	var rows ui.ListState
	var win *mygo.Window
	s.operations++
	copyText := func(c *ui.Context, value string) { mygo.Clipboard.WriteText(value); c.Toast("文字已复制") }
	win = mygo.NewWindow(mygo.WindowOptions{Title: "ScreenPin 文字识别", Width: 680, Height: 580, MinWidth: 380, MinHeight: 300, UseContentSize: true, Content: ui.View(func(c *ui.Context) {
		panelTheme(c)
		if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
			win.Close()
			return
		}
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, "识别结果").FontWeight(600)
				ui.Text(c, fmt.Sprintf("本地识别 · %d 行", len(result.Lines))).FontSize(12).TextColor(c.Theme().TextMuted)
			})
			ui.Tabs(c, &tab, "全文", "逐行复制")
			if tab == 0 {
				ui.TextArea(c.Key("ocr-text"), &text).Label("识别文字，可编辑后复制").Grow(1).MinHeight(0).FillWidth()
			} else {
				ui.List(c.Key("ocr-lines"), &rows, len(result.Lines), func(i int) {
					ui.Row(c).Gap(8).Padding(6, 0).AlignItems(ui.Center).Children(func() {
						ui.Text(c, result.Lines[i].Text).Selectable().Grow(1)
						if ui.Button(c, "复制").Label(fmt.Sprintf("复制第 %d 行", i+1)).Clicked() {
							copyText(c, result.Lines[i].Text)
						}
					})
				}).Grow(1).MinHeight(0).FillWidth()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "关闭").Clicked() {
					win.Close()
				}
				if ui.PrimaryButton(c, "复制全文").Clicked() {
					copyText(c, text)
				}
			})
		})
	})})
	win.OnClosed(func() { result.Lines = nil; text = ""; rows = ui.ListState{}; s.operations--; s.maybeQuit() })
}
