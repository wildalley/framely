package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"image"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/ocr"
	"github.com/wildalley/framely/internal/ocrtext"
	"github.com/wildalley/framely/internal/translate"
)

func (s *application) recognizePicture(picture editor.Picture) { s.recognize(picture, false) }

// translatePicture recognizes the picture's text and translates it.
func (s *application) translatePicture(picture editor.Picture) { s.recognize(picture, true) }

func (s *application) recognize(picture editor.Picture, translate bool) {
	defer picture.Close()
	retained := picture.Retain()
	ctx, cancel := context.WithCancel(s.ctx)
	progress, complete := s.startOCRWith(cancel, translate)
	pixels := picture.OCRPixels()
	s.worker(func() {
		defer retained.Close()
		defer cancel()
		result, err := ocr.RecognizePrepared(ctx, func(context.Context) (*image.RGBA, error) { return pixels, nil }, progress)
		complete(result, err)
	})
}

type ocrWindow struct {
	win             *mygo.Window
	cancel          context.CancelFunc
	closed, running bool
	status, text    string
	tab             int
	characters      bool
	selection       *ocrtext.Selection
	rows            [][2]int
	list            ui.ListState
	anchor          int
	preview         string
	selected        int
	copyText        func(string) error
	closeWindow     func()
	hits            []ocrWordHit
	hitUsed         int
	sweep           ocrSweep

	// Translation, on the third tab. translator runs off the main thread
	// through run; later brings its result back while the window is open.
	translator    func(ctx context.Context, text, target string) (translate.Result, error)
	run, later    func(func())
	openSettings  func()
	provider      string // the service's name, empty when none is set
	offline       bool   // the service is a model on this computer
	autoTranslate bool   // switch to the translation once text is recognized
	target        string // translate.Auto, Chinese or English
	tr            ocrTranslation
}

// ocrTranslation is the translation of source, or its progress or error.
type ocrTranslation struct {
	running bool
	cancel  context.CancelFunc
	source  string
	text    string
	err     string
}

func newOCRWindow() *ocrWindow {
	return &ocrWindow{selection: ocrtext.New("", false), anchor: -1, copyText: func(text string) error { mygo.Clipboard.WriteText(text); return nil }}
}

func (s *application) openOCR() *ocrWindow {
	if s.scroll != nil && s.scroll.running {
		s.scroll.show()
	}
	v := newOCRWindow()
	s.operations++
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: brand.Title("文字识别"), Width: 760, Height: 620, MinWidth: 400, MinHeight: 400, UseContentSize: true, Content: ui.View(v.view)})
	v.closeWindow = func() { v.win.Close() }
	v.win.OnClosed(func() { v.release(); s.operations--; s.maybeQuit() })
	v.provider = translate.Provider(s.cfg.TranslateProvider).Label()
	if s.cfg.TranslateProvider == "" {
		v.provider = ""
	}
	v.target = s.cfg.TranslateTarget
	v.offline = s.cfg.TranslateProvider == string(translate.Offline)
	cfg := s.cfg
	v.translator = func(ctx context.Context, text, target string) (translate.Result, error) {
		return translateWith(ctx, cfg, text, target)
	}
	v.run = s.worker
	v.later = func(fn func()) {
		mygo.RunOnMain(func() {
			if !v.closed && s.ctx.Err() == nil {
				fn()
				v.win.Invalidate()
			}
		})
	}
	v.openSettings = func() { s.showSettings("") }
	return v
}

// startTranslate translates the recognized text, replacing a translation
// still running.
func (v *ocrWindow) startTranslate() {
	text := strings.TrimSpace(v.text)
	if v.closed || text == "" || v.translator == nil {
		return
	}
	if v.tr.cancel != nil {
		v.tr.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.tr = ocrTranslation{running: true, cancel: cancel, source: v.text}
	translator, target, later := v.translator, v.target, v.later
	v.run(func() {
		result, err := translator(ctx, text, target)
		later(func() {
			if v.tr.cancel == nil || ctx.Err() != nil {
				return // Replaced, cancelled or closed.
			}
			cancel()
			v.tr.running, v.tr.cancel = false, nil
			if err != nil {
				v.tr.err = err.Error()
				return
			}
			v.tr.text = result.Text
		})
	})
}

func (s *application) showOCR(result ocr.Result) { v := s.openOCR(); v.finish(result, nil) }

func (s *application) startOCR(cancel context.CancelFunc) (func(ocr.Progress), func(ocr.Result, error)) {
	return s.startOCRWith(cancel, false)
}

// startOCRWith opens the text window for a recognition; with translate,
// it shows the translation once the text is in.
func (s *application) startOCRWith(cancel context.CancelFunc, translate bool) (func(ocr.Progress), func(ocr.Result, error)) {
	v := s.openOCR()
	v.autoTranslate = translate
	v.cancel = cancel
	v.running = true
	v.status = "等待识别…"
	dispatch := func(fn func()) {
		mygo.RunOnMain(func() {
			if !v.closed && s.ctx.Err() == nil {
				fn()
				v.win.Invalidate()
			}
		})
	}
	// Merge and segment on the OCR goroutine; dispatch only prepared text to
	// the UI, so model progress cannot stall selection or window movement.
	var lines []ocr.Line
	return func(p ocr.Progress) {
			var next *ocrText
			if len(p.Lines) > 0 {
				lines = append(lines, p.Lines...)
				next = prepareOCRText((ocr.Result{Lines: ocr.MergeLines(lines)}).Text())
			}
			dispatch(func() { v.progress(p, next) })
		}, func(r ocr.Result, err error) {
			lines = nil
			var next *ocrText
			if err == nil {
				next = prepareOCRText(r.Text())
			}
			dispatch(func() { v.finishPrepared(r, err, next) })
		}
}

func (v *ocrWindow) release() {
	if v.closed {
		return
	}
	v.closed = true
	v.running = false
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.rows = nil
	v.text = ""
	v.preview = ""
	v.selection = nil
	v.list = ui.ListState{}
	v.copyText = nil
	v.closeWindow = nil
	v.hits, v.hitUsed, v.sweep = nil, 0, ocrSweep{}
	if v.tr.cancel != nil {
		v.tr.cancel()
	}
	v.tr = ocrTranslation{}
	v.translator, v.run, v.later, v.openSettings = nil, nil, nil, nil
}

func (v *ocrWindow) progress(p ocr.Progress, next *ocrText) {
	if !v.running {
		return
	}
	switch p.Stage {
	case "queued":
		v.status = "已排队，等待上一张图片完成…"
	case "preparing":
		v.status = "正在准备选区…"
	case "recognizing":
		v.status = "正在本地识别…"
		if p.Total > 0 {
			v.status = fmt.Sprintf("正在识别 %d / %d 区块", p.Completed, p.Total)
		}
	}
	if next != nil {
		v.setPrepared(next, true)
	}
}

func (v *ocrWindow) finish(result ocr.Result, err error) {
	var next *ocrText
	if err == nil {
		next = prepareOCRText(result.Text())
	}
	v.finishPrepared(result, err, next)
}

func (v *ocrWindow) finishPrepared(result ocr.Result, err error, next *ocrText) {
	v.running = false
	v.cancel = nil
	if err != nil {
		if errors.Is(err, context.Canceled) {
			v.status = "识别已取消，已完成的文字仍可使用"
		} else {
			v.status = "识别失败：" + err.Error()
		}
		return
	}
	v.setPrepared(next, true)
	v.status = fmt.Sprintf("识别完成 · %d 行 · 点选文字，组合复制", len(result.Lines))
	if len(result.Lines) == 0 {
		v.status = "未找到文字，可缩小选区后重试"
		return
	}
	if v.autoTranslate {
		v.autoTranslate = false
		v.tab = 2
		if v.provider != "" {
			v.startTranslate()
		}
	}
}

func (v *ocrWindow) setText(text string, preserve bool) {
	v.setPrepared(prepareOCRText(text), preserve)
}

type ocrText struct{ words, characters *ocrtext.Selection }

func prepareOCRText(text string) *ocrText {
	return &ocrText{words: ocrtext.New(text, false), characters: ocrtext.New(text, true)}
}

func (v *ocrWindow) setPrepared(text *ocrText, preserve bool) {
	v.sweep = ocrSweep{}
	old := v.selection
	next := text.words
	if v.characters {
		next = text.characters
	}
	if preserve && old != nil {
		next.PreserveFrom(old)
	}
	v.text = next.Text
	v.selection = next
	v.rows = next.Rows()
	v.anchor = -1
	v.refreshSelection()
}

func (v *ocrWindow) refreshSelection() {
	v.selected = v.selection.Count()
	v.preview = v.selection.SelectedText()
}
func (v *ocrWindow) copy(c *ui.Context, text string) {
	if text == "" {
		return
	}
	if err := v.copyText(text); err != nil {
		c.Toast("复制失败：" + err.Error())
	} else {
		c.Toast("已复制")
	}
}

func (v *ocrWindow) view(c *ui.Context) {
	if v.closed {
		return
	}
	panelTheme(c)
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if v.closeWindow != nil {
			v.closeWindow()
		}
		return
	}
	if v.tab == 2 && c.Shortcut(ui.Cmd, ui.KeyC) {
		v.copy(c, v.tr.text)
	}
	if v.tab == 0 {
		if c.Shortcut(ui.Cmd, ui.KeyA) {
			v.selection.All(true)
			v.refreshSelection()
		}
		if c.Shortcut(ui.Cmd, ui.KeyC) {
			v.copy(c, v.preview)
		}
	}
	ui.Column(c).Fill().Padding(16).Gap(10).Children(func() {
		subtitle := "离线识别 · 不上传图片"
		if v.tab == 2 && v.offline {
			subtitle = "本机离线翻译 · 不联网"
		} else if v.tab == 2 && v.provider != "" {
			subtitle = "译文由 " + v.provider + " 生成 · 只发送文字，不发送图片"
		}
		panelHeader(c, "提取文字", subtitle, func() {
			if v.running && ui.Button(c, "取消识别").Clicked() {
				if v.cancel != nil {
					v.cancel()
				}
				v.status = "正在取消…"
			}
		})
		ui.Text(c, v.status).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(2)
		previousTab := v.tab
		ui.Tabs(c, &v.tab, "选词复制", "全文编辑", "翻译")
		if previousTab != v.tab {
			v.sweep = ocrSweep{}
			if v.tab == 2 && v.provider != "" && !v.running && !v.tr.running && v.tr.source != v.text {
				v.startTranslate()
			}
		}
		if previousTab == 1 && v.tab == 0 && v.text != v.selection.Text {
			v.setText(v.text, false)
		}
		if v.tab == 0 {
			ui.Row(c).Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "全选").Disabled(len(v.rows) == 0).Clicked() {
					v.selection.All(true)
					v.refreshSelection()
				}
				if ui.Button(c, "反选").Disabled(len(v.rows) == 0).Clicked() {
					v.selection.Invert()
					v.refreshSelection()
				}
				if ui.Button(c, "清空").Disabled(v.selected == 0).Clicked() {
					v.selection.All(false)
					v.refreshSelection()
				}
				ui.Box(c).Width(1).Height(18).Margin(0, 2).Background(c.Theme().Border).Shrink(0)
				charToggle := ui.Toggle(c, &v.characters, "单字").Tooltip("拆成单字，精确选择中文或符号")
				if v.characters {
					charToggle.Background(brand.Soft(c)).TextColor(brand.Ink(c)).Border(1, c.Theme().Accent)
				}
				if charToggle.Changed() {
					v.sweep = ocrSweep{}
					v.selection.SetCharacters(v.characters)
					v.rows = v.selection.Rows()
					v.anchor = -1
					v.refreshSelection()
				}
			})
			if len(v.rows) == 0 {
				ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "文字会出现在这里").FontSize(16)
					ui.Text(c, "识别完成一部分，即可开始选词").FontSize(12).TextColor(c.Theme().TextMuted)
				})
			} else {
				v.hitUsed = 0
				ui.List(c.Key("ocr-words"), &v.list, len(v.rows), func(row int) {
					rangeRow := v.rows[row]
					ui.Row(c).FillWidth().Wrap().Gap(6).Padding(4, 0).FocusGroup(ui.Horizontal | ui.Vertical).Label(fmt.Sprintf("文字块第 %d 组", row+1)).Children(func() {
						for i := rangeRow[0]; i < rangeRow[1]; i++ {
							t := &v.selection.Tokens[i]
							chip := ui.Toggle(c.Key(fmt.Sprintf("word-%d", i)), &t.Selected, "").Label(fmt.Sprintf("文字块 %d：%s", i+1, t.Text)).MinHeight(34).Padding(7, 10).Radius(7).MaxWidthPercent(100)
							if v.hitUsed == len(v.hits) {
								v.hits = append(v.hits, ocrWordHit{})
							}
							hit := &v.hits[v.hitUsed]
							hit.index = i
							chip.Bind(&hit.handle)
							v.hitUsed++
							if t.Selected {
								chip.Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Border(1, c.Theme().Accent)
							} else {
								chip.Background(c.Theme().Surface).Border(1, c.Theme().Border)
							}
							chip.Children(func() { ui.Text(c, t.Text).MaxLines(3) })
							if chip.Changed() {
								if chip.ClickModifiers()&ui.Shift != 0 && v.anchor >= 0 {
									v.selection.Range(v.anchor, i, t.Selected)
								}
								v.anchor = i
								v.refreshSelection()
							}
						}
					})
				}).Grow(1).MinHeight(50).FillWidth().HandleInput(func(ev ui.InputEvent) bool { return v.sweepInput(c, ev) })
			}
			ui.Text(c, "点选 / 拖动扫选 / Shift 连选 · 方向键与空格").FontSize(12).MaxLines(2).TextColor(c.Theme().TextMuted)
			ui.Column(c).FillWidth().Padding(10).Gap(5).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
				ui.Text(c, fmt.Sprintf("已选 %d 块", v.selected)).FontSize(12).FontWeight(600)
				preview := v.preview
				if preview == "" {
					preview = "选中的文字会按原文顺序组合"
				}
				ui.Text(c, preview).MaxLines(2).FontSize(13)
			})
		} else if v.tab == 1 {
			ui.TextArea(c.Key("ocr-text"), &v.text).Label("识别文字，可编辑后复制").Disabled(v.running).Grow(1).MinHeight(50).FillWidth()
		} else {
			v.translationView(c)
		}
		ui.Row(c).Wrap().Gap(8).Justify(ui.End).Children(func() {
			if v.tab == 2 {
				if ui.Button(c, "复制原文").Disabled(strings.TrimSpace(v.text) == "").Clicked() {
					v.copy(c, v.text)
				}
				if ui.PrimaryButton(c, "复制译文").Disabled(v.tr.text == "" || v.tr.running).Clicked() {
					v.copy(c, v.tr.text)
				}
				return
			}
			if ui.Button(c, "复制全文").Disabled(strings.TrimSpace(v.text) == "").Clicked() {
				v.copy(c, v.text)
			}
			if v.tab == 0 && ui.PrimaryButton(c, "复制选中").Disabled(v.selected == 0).Clicked() {
				v.copy(c, v.preview)
			}
		})
	})
}

var targetLabels = []string{"自动", "译成中文", "译成英文"}
var targetValues = []string{translate.Auto, translate.Chinese, translate.English}

// translationView shows the recognized text beside its translation, or
// what is needed before translating.
func (v *ocrWindow) translationView(c *ui.Context) {
	t := c.Theme()
	if v.provider == "" {
		ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, "还没有设置翻译服务").FontSize(16)
			ui.Text(c, "选择 DeepL、百度、腾讯或兼容 OpenAI 的大模型，填写自己的密钥。只发送识别出的文字。").FontSize(12).TextColor(t.TextMuted).MaxLines(3)
			if ui.PrimaryButton(c, "设置翻译服务").Clicked() && v.openSettings != nil {
				v.openSettings()
			}
		})
		return
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		choice := targetLabels[0]
		for i, value := range targetValues {
			if value == v.target {
				choice = targetLabels[i]
			}
		}
		if ui.Select(c.Key("translate-target"), &choice, targetLabels).Label("翻译方向").Width(120).Changed() {
			for i, label := range targetLabels {
				if label == choice {
					v.target = targetValues[i]
				}
			}
			v.startTranslate()
		}
		status := "由 " + v.provider + " 翻译"
		switch {
		case v.running:
			status = "等待识别完成…"
		case v.tr.running:
			status = "正在翻译…"
		case v.tr.err != "":
			status = "翻译失败：" + v.tr.err
		case v.tr.source != "" && v.tr.source != v.text:
			status = "原文已修改，可重新翻译"
		}
		color := t.TextMuted
		if v.tr.err != "" && !v.tr.running {
			color = t.Danger
		}
		ui.Text(c, status).FontSize(12).TextColor(color).MaxLines(2).Grow(1).MinWidth(0).Selectable()
		if ui.Button(c.Key("translate-again"), "重新翻译").Disabled(v.running || strings.TrimSpace(v.text) == "").Clicked() {
			v.startTranslate()
		}
	})
	w, _ := c.Size()
	pane := func(key, caption, text, empty string) {
		ui.Column(c.Key(key)).Grow(1).Basis(0).MinWidth(0).MinHeight(60).FillHeight().Padding(10).Gap(6).Radius(8).Background(t.Surface).Border(1, t.Border).Children(func() {
			ui.Text(c, caption).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
			ui.Scroll(c.Key(key + "-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				if text == "" {
					ui.Text(c, empty).FontSize(13).TextColor(t.TextMuted)
				} else {
					ui.Text(c, text).FontSize(14).LineHeight(1.6).Selectable().FillWidth()
				}
			})
		})
	}
	body := ui.Row(c.Key("translation")).Grow(1).MinHeight(0).FillWidth().Gap(10).AlignItems(ui.Stretch)
	if w < 560 {
		body = ui.Column(c.Key("translation")).Grow(1).MinHeight(0).FillWidth().Gap(10).AlignItems(ui.Stretch)
	}
	body.Children(func() {
		pane("translate-source", "原文", v.text, "识别出的文字会出现在这里")
		translated := v.tr.text
		if v.tr.running {
			translated = ""
		}
		pane("translate-result", "译文", translated, "译文会出现在这里")
	})
}
