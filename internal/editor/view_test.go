package editor

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
)

func TestIconGlyphsAreCenteredInsideTheirButtons(t *testing.T) {
	for _, toolbar := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, scale := range []float32{1, 1.5, 2} {
				t.Run(fmt.Sprintf("toolbar=%v/dark=%v/scale=%v", toolbar, dark, scale), func(t *testing.T) {
					tt := ui.NewTester(func(c *ui.Context) {
						controls := func() {
							iconButton(c, "button", iconRect, "按钮", false)
							on := false
							iconToggle(c, "toggle", iconRect, "切换", &on, false)
						}
						ui.Row(c).Padding(8).Gap(8).Children(func() {
							if toolbar {
								ui.Toolbar(c, controls)
							} else {
								controls()
							}
						})
					}, 200, 80)
					tt.SetDark(dark)
					tt.SetScale(scale)
					img := tt.Image()
					for _, label := range []string{"按钮", "切换"} {
						r, ok := tt.Find(label)
						if !ok || r.W != 32 || r.H != 32 {
							t.Fatalf("button bounds: %+v, found=%v", r, ok)
						}
						minX, minY, maxX, maxY := img.Bounds().Max.X, img.Bounds().Max.Y, -1, -1
						for y := int(r.Y * scale); y < int((r.Y+r.H)*scale); y++ {
							for x := int(r.X * scale); x < int((r.X+r.W)*scale); x++ {
								c := img.RGBAAt(x, y)
								glyph := c.R < 100 && c.G < 100 && c.B < 100
								if dark {
									glyph = c.R > 180 && c.G > 180 && c.B > 180
								}
								if glyph && c.A > 200 {
									minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
								}
							}
						}
						x := float64(minX+maxX+1) / 2 / float64(scale)
						y := float64(minY+maxY+1) / 2 / float64(scale)
						if maxX < 0 || math.Abs(x-float64(r.X+r.W/2)) > 1 || math.Abs(y-float64(r.Y+r.H/2)) > 1 {
							t.Fatalf("%s glyph center (%g,%g) is outside button center: %+v", label, x, y, r)
						}
					}
				})
			}
		}
	}
}

func TestSaveMenuSharesQualityAcrossJPEGAndWebP(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	tt := ui.NewTester(a.view, 320, 300)
	label := "保存图片 (" + commandKey() + "+S)"
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "保存 WebP（92%）") {
		t.Fatalf("WebP is missing from the save menu: %v", tt.Menu())
	}
	if err := tt.ChooseMenuItem("JPEG / WebP 质量", "80%"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"保存 PNG", "保存 JPEG（80%）", "保存 WebP（80%）"} {
		if !slices.Contains(tt.Menu(), want) {
			t.Errorf("missing %q after changing quality: %v", want, tt.Menu())
		}
	}
}

func TestNarrowImageKeepsActionsAndOverflowTools(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	tt := ui.NewTester(a.view, 320, 300)
	for _, label := range []string{"样式", "新截图", "长截图", "识别文字", "设置与快捷键", "置顶", "保存图片 (" + commandKey() + "+S)", "复制", "关闭图片"} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.X < 0 || r.X+r.W > 320 || r.Y < 0 || r.Y+r.H > 300 {
			t.Fatalf("%q is unavailable in a narrow window: %+v", label, r)
		}
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("文字 (T)"); err != nil {
		t.Fatal(err)
	}
	if a.tool != doc.ToolText {
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

func TestCaptureActionsStayVisibleAndKeepTheirTargetsAcrossLayouts(t *testing.T) {
	for _, width := range []int{320, 420, 759, 760, 1100} {
		for _, dark := range []bool{false, true} {
			a, _ := textApp()
			capture, scroll, settings := 0, 0, 0
			a.OnCapture = func() { capture++ }
			a.OnScroll = func() { scroll++ }
			a.OnSettings = func() { settings++ }
			tt := ui.NewTester(a.view, width, 300)
			tt.SetDark(dark)
			for _, label := range []string{"新截图", "长截图", "识别文字", "设置与快捷键", "复制", "关闭图片"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+.5 || r.Y+r.H > 300.5 {
					t.Fatalf("width=%d dark=%v %s: %+v found=%v", width, dark, label, r, ok)
				}
			}
			for _, label := range []string{"新截图", "长截图", "设置与快捷键"} {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
			}
			if capture != 1 || scroll != 1 || settings != 1 {
				t.Fatalf("callbacks: %d %d %d", capture, scroll, settings)
			}
			a.busy = true
			tt.Frame()
			_ = tt.Click("长截图")
			_ = tt.Click("新截图")
			if capture != 1 || scroll != 1 {
				t.Fatal("busy image launched another capture")
			}
			a.release()
		}
	}
}

func TestCanvasUsesANativeMenu(t *testing.T) {
	a, _ := textApp()
	defer a.release()
	tt := ui.NewTester(a.view, 500, 400)
	canvas, ok := tt.Find("图片画布")
	if !ok {
		t.Fatal("missing canvas")
	}
	tt.RightClickAt(canvas.X+30, canvas.Y+30)
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
