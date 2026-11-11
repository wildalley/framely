package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/ocr"
	"github.com/wildalley/framely/internal/ocrtext"
)

func TestOCRWordsSelectRangeCopyAndEdit(t *testing.T) {
	v := newOCRWindow()
	v.setText("Hello brave world\n你好世界 ScreenPin 2026", false)
	var copied string
	v.copyText = func(text string) error { copied = text; return nil }
	tt := ui.NewTester(v.view, 760, 620)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("复制选中")
	if copied != "" {
		t.Fatal("empty selection copied")
	}
	click("文字块 1：Hello")
	if err := tt.ClickWith(ui.Shift, "文字块 3：world"); err != nil {
		t.Fatal(err)
	}
	click("复制选中")
	if copied != "Hello brave world" {
		t.Fatalf("range copy: %q", copied)
	}
	click("单字")
	if v.preview != copied {
		t.Fatalf("character mode lost selection: %q", v.preview)
	}
	click("清空")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if copied != v.text {
		t.Fatalf("select all lost source whitespace: %q", copied)
	}
	click("全文编辑")
	click("识别文字，可编辑后复制")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("修改文字 edited text")
	click("复制全文")
	if copied != "修改文字 edited text" {
		t.Fatalf("edited text: %q", copied)
	}
	click("选词复制")
	if v.selection.Text != copied || v.selected != 0 {
		t.Fatal("editing did not reset word selection")
	}
}

func TestOCRProgressCancellationAndRelease(t *testing.T) {
	v := newOCRWindow()
	ctx, cancel := context.WithCancel(context.Background())
	v.running, v.cancel = true, cancel
	v.progress(ocr.Progress{Stage: "recognizing", Completed: 1, Total: 2}, prepareOCRText("Hello world"))
	v.selection.Tokens[0].Selected = true
	v.refreshSelection()
	v.progress(ocr.Progress{Stage: "recognizing", Completed: 2, Total: 2}, prepareOCRText("Hello world\n你好世界"))
	if v.preview != "Hello" || v.selected != 1 {
		t.Fatal("progress changed the selected text")
	}
	tt := ui.NewTester(v.view, 400, 400)
	if err := tt.Click("取消识别"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("cancel button did not stop the request")
	}
	v.finish(ocr.Result{}, ctx.Err())
	if v.running || v.preview != "Hello" {
		t.Fatal("cancelled request lost partial text or retained raw lines")
	}
	ctx, cancel = context.WithCancel(context.Background())
	v.cancel = cancel
	v.release()
	v.release()
	if ctx.Err() == nil || v.selection != nil || v.rows != nil || v.text != "" || v.copyText != nil {
		t.Fatal("window retained OCR state after closing")
	}
}

func TestOCRWindowLayout(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		dark          bool
		scale         float32
	}{
		{"light", 760, 620, false, 1}, {"dark", 760, 620, true, 1},
		{"compact", 400, 400, false, 1}, {"large-text", 400, 500, true, 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newOCRWindow()
			v.finish(ocr.Result{Lines: []ocr.Line{{Text: "截图之后，让文字自由组合。"}, {Text: "ScreenPin 支持本地识别、选词复制与全文编辑。"}, {Text: "版本 0.2.1  邮箱 hello@example.com"}, {Text: "https://example.com/" + strings.Repeat("long-path/", 20)}}}, nil)
			v.selection.Range(0, 2, true)
			v.refreshSelection()
			tt := ui.NewTester(v.view, tc.width, tc.height)
			tt.SetDark(tc.dark)
			tt.SetPreferences(ui.Preferences{TextScale: tc.scale, ReduceMotion: true})
			for _, label := range []string{"选词复制", "全文编辑", "全选", "反选", "清空", "单字", "复制全文", "复制选中"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(tc.width)+.5 || r.Y+r.H > float32(tc.height)+.5 {
					t.Fatalf("%s clipped: %+v", label, r)
				}
			}
			if out := os.Getenv("SCREENPIN_UI_SCREENSHOTS"); out != "" {
				if err := os.MkdirAll(out, 0755); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(out, fmt.Sprintf("ocr-%s.png", tc.name)))
				if err != nil {
					t.Fatal(err)
				}
				if err = png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOCRClosedWindowsReleaseLargeSelections(t *testing.T) {
	var windows []*ocrWindow
	var pointers []weak.Pointer[ocrtext.Token]
	round := func() {
		v := newOCRWindow()
		v.setText(strings.Repeat("ScreenPin 本地文字识别选词复制\n", 500), false)
		pointers = append(pointers, weak.Make(&v.selection.Tokens[0]))
		tt := ui.NewTester(v.view, 400, 400)
		_ = tt.Image()
		v.release()
		tt.Frame()
		windows = append(windows, v)
	}
	for range 20 {
		round()
	}
	runtime.GC()
	for _, p := range pointers {
		if p.Value() != nil {
			t.Fatal("closed OCR window retained word tokens")
		}
	}
	runtime.KeepAlive(windows)
}
