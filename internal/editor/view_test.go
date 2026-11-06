package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
)

func TestNarrowImageKeepsActionsAndOverflowTools(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	tt := ui.NewTester(a.view, 320, 300)
	for _, label := range []string{"样式", "置顶", "保存图片 (" + commandKey() + "+S)", "复制", "关闭图片"} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.X < 0 || r.X+r.W > 320 || r.Y < 0 || r.Y+r.H > 300 {
			t.Fatalf("%q is unavailable in a narrow window: %+v", label, r)
		}
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("画笔 (P)"); err != nil {
		t.Fatal(err)
	}
	if a.tool != doc.ToolPen {
		t.Fatal("an overflow item failed to activate its drawing tool")
	}
	// The toolbar is one tab stop; its arrow keys move focus among controls.
	if err := tt.Click("选择 (V)"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeySpace)
	if a.tool != doc.ToolRectangle {
		t.Fatalf("toolbar arrow navigation did not choose the next tool: %s", a.tool)
	}
	if err := tt.Click("关闭图片"); err != nil {
		t.Fatal(err)
	}
	if !a.closed {
		t.Fatal("the close action only hid the image")
	}
}

func TestCanvasUsesANativeMenu(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	tt := ui.NewTester(a.view, 500, 400)
	tt.RightClickAt(30, 30)
	if err := tt.ChooseMenuItem("删除标注"); err != nil {
		t.Fatal(err)
	}
	if len(a.history.Present()) != 0 {
		t.Fatal("the native context menu did not delete the annotation")
	}
	a.undo()
	if len(a.history.Present()) != 1 {
		t.Fatal("the menu action did not keep undo history")
	}
}
