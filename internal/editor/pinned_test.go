package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/doc"
)

func TestPinnedImageHasNoChromeUntilHovered(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	a.pinned, a.selected = true, ""
	tt := ui.NewTester(a.view, 500, 300)
	for _, label := range []string{"标注工具", "图片操作", "钉图操作"} {
		if r, ok := tt.Find(label); ok && r.W > 0 {
			t.Fatalf("a pinned image shows %s before the pointer rests on it", label)
		}
	}
	tt.Move(250, 150)
	bar, ok := tt.Find("钉图操作")
	if !ok || bar.W <= 0 || bar.Y+bar.H > 300 || bar.X < 0 || bar.X+bar.W > 500 {
		t.Fatalf("hovering shows no bar inside the image: %+v", bar)
	}
	if err := tt.Click("关闭图片"); err != nil {
		t.Fatal(err)
	}
	if !a.closed {
		t.Fatal("the pinned bar's close only hid the image")
	}
}

func TestPinnedImageZoomsWithTheWheelAndIgnoresToolKeys(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	a.pinned, a.selected, a.zoom = true, "", 1
	tt := ui.NewTester(a.view, 500, 300)
	tt.Wheel(250, 150, 0, -1)
	if a.zoom <= 1 {
		t.Fatalf("the wheel did not zoom the pinned image: %g", a.zoom)
	}
	tt.Key(0, ui.KeyR)
	if a.tool != doc.ToolSelect {
		t.Fatal("a tool key changed the tool of a pinned image")
	}
}

func TestTinyPinnedImageLeavesActionsToItsMenu(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	a.pinned, a.selected = true, ""
	tt := ui.NewTester(a.view, 100, 40)
	tt.Move(50, 20)
	if r, ok := tt.Find("钉图操作"); ok && r.W > 0 {
		t.Fatal("the hover bar covers a tiny pinned image")
	}
	tt.RightClickAt(50, 20)
	if len(tt.Menu()) == 0 {
		t.Fatal("a tiny pinned image has no menu")
	}
}
