package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/doc"
)

func textApp() (*App, doc.Annotation) {
	a := New(config.Default())
	an := doc.New(doc.ToolText, doc.Point{X: 10, Y: 10}, doc.DefaultStyle(doc.ToolText))
	an.SetText("原始文字")
	a.image = &doc.Image{Width: 500, Height: 300}
	a.history.Reset([]doc.Annotation{an})
	a.selected = an.ID
	return a, an
}

func TestAnnotationTextKeepsTypingAndCommitsOnBlur(t *testing.T) {
	a, original := textApp()
	a.startEditing(original.ID)
	tt := ui.NewTester(func(c *ui.Context) {
		a.shortcuts(c)
		ui.Box(c).Fill().Children(func() {
			if a.editingID != "" {
				a.textEditor(c, 1)
			}
			ui.Button(c, "离开输入框").Absolute().Top(200)
		})
	}, 500, 300)
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("the annotation editor did not receive focus")
	}
	tt.TypeKey(0, ui.KeyR, "r")
	if a.tool != doc.ToolSelect || a.editingID != original.ID || a.editingText != "r" {
		t.Fatalf("typing activated an editor shortcut or lost the initial selection: tool=%s, text=%q", a.tool, a.editingText)
	}
	tt.Type("新文字")
	tt.Key(0, ui.KeyBackspace)
	if len(a.history.Present()) != 1 || a.editingText != "r新文" {
		t.Fatal("Backspace deleted the annotation instead of editing its text")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "r新文" {
		t.Fatalf("copy did not stay in the text input: %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyV)
	if a.busy || a.editingText != "r新文" || a.editingID != original.ID {
		t.Fatal("pasting text was handled as an image replacement")
	}
	if err := tt.Click("离开输入框"); err != nil {
		t.Fatal(err)
	}
	if a.editingID != "" || a.history.Present()[0].TextValue() != "r新文" {
		t.Fatal("leaving the input did not finish the text edit")
	}
	a.undo()
	if got := a.history.Present()[0].TextValue(); got != original.TextValue() {
		t.Fatalf("the text edit did not undo in one step: %q", got)
	}
}

func TestAnnotationTextFinishesWithKeyboard(t *testing.T) {
	for _, key := range []struct {
		name string
		mods ui.Modifiers
		key  ui.Key
	}{{"escape", 0, ui.KeyEscape}, {"command-enter", ui.Cmd, ui.KeyEnter}} {
		t.Run(key.name, func(t *testing.T) {
			a, original := textApp()
			a.startEditing(original.ID)
			tt := ui.NewTester(func(c *ui.Context) {
				a.shortcuts(c)
				ui.Box(c).Fill().Children(func() {
					if a.editingID != "" {
						a.textEditor(c, 1)
					}
				})
			}, 500, 300)
			tt.Type("修改")
			tt.Key(key.mods, key.key)
			if a.editingID != "" || a.selected != original.ID {
				t.Fatal("finishing text also cleared the annotation selection")
			}
			a.undo()
			if a.history.Present()[0].TextValue() != original.TextValue() {
				t.Fatal("the committed text edit did not undo")
			}
		})
	}
}

func TestClosingStylePanelCommitsTextAsOneStep(t *testing.T) {
	a, original := textApp()
	a.panel = PanelStyle
	tt := ui.NewTester(func(c *ui.Context) {
		a.shortcuts(c)
		ui.Column(c).Fill().Children(func() {
			if a.panel == PanelStyle {
				a.stylePanel(c, a.selectedAnnotation().Style())
			}
			if ui.Button(c, "关闭样式面板").Clicked() {
				a.togglePanel(PanelStyle)
			}
		})
	}, 500, 500)
	if err := tt.Click("标注文字"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.TypeKey(0, ui.KeyR, "r")
	tt.Type("样式面板修改")
	if a.tool != doc.ToolSelect || !a.textEdit {
		t.Fatal("style-panel typing activated a tool or lost its transaction")
	}
	if err := tt.Click("关闭样式面板"); err != nil {
		t.Fatal(err)
	}
	if a.history.InTransaction() || a.textEdit {
		t.Fatal("hiding the field left its history transaction open")
	}
	a.undo()
	if a.history.Present()[0].TextValue() != original.TextValue() {
		t.Fatal("the style text did not undo in one step")
	}
}
