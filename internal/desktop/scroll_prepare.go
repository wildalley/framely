package desktop

import (
	"context"
	"errors"
	"image"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/editor"
)

type scrollRequest struct {
	options  capture.StreamOptions
	display  mygo.Display
	displays []mygo.Display
	reuse    *editor.ScreenSelection
}

// scrollCapture owns preparation pixels and one native source. The same Close
// is shared with the session, so failed preparation and review release it once.
type scrollCapture struct {
	capture.Stream
	closeOnce           sync.Once
	baseline            *image.RGBA
	screen              *mygo.Display
	region              image.Rectangle
	size                image.Point
	automatic, fallback bool
}

func (s *scrollCapture) Close() { s.closeOnce.Do(s.Stream.Close) }

type desktopScrollSelection struct {
	display     mygo.Display
	region      image.Rectangle
	desktopSize image.Point
	pixels      *image.RGBA
}

// With permission, selecting on the desktop first identifies the intended
// monitor even when Wayland withholds global pointer coordinates.
func (v *scrollWindow) pickDesktopScrollRegion(ctx context.Context, displays []mygo.Display) (*desktopScrollSelection, error) {
	if capture.ScreenshotAccessContext(ctx) != capture.AccessGranted {
		return nil, ctx.Err()
	}
	if err := waitForCapture(ctx, 180*time.Millisecond); err != nil {
		return nil, err
	}
	data, err := capture.ScreenshotContext(ctx, false)
	if err != nil {
		if cancelled(err) || ctx.Err() != nil {
			return nil, err
		}
		log.Printf(brand.Name+": long screenshot desktop frame unavailable: %v; selecting shared source first", err)
		return nil, nil
	}
	full, err := capture.Decode(data)
	if err != nil {
		log.Printf(brand.Name+": long screenshot desktop frame: %v; selecting shared source first", err)
		return nil, nil
	}
	region, target, err := editor.PickScreenRegion(ctx, full, displays, "", image.Pt(80, 160), "框选滚动内容，松开开始 · 首次共享请选择此屏 · Esc 取消")
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errors.New("无法确定框选所在的显示器")
	}
	for _, display := range displays {
		if display.Bounds == *target {
			pixels, err := capture.CropPixels(full, region)
			if err != nil {
				return nil, err
			}
			log.Printf(brand.Name+": long screenshot desktop selection: id=%d display=%v region=%v", display.ID, display.Bounds, region)
			return &desktopScrollSelection{display: display, region: region, desktopSize: full.Bounds().Size(), pixels: pixels}, nil
		}
	}
	return nil, errors.New("框选所在的显示器已断开")
}

func (v *scrollWindow) prepareScrollCapture(ctx context.Context, request scrollRequest) (*scrollCapture, error) {
	var selected *desktopScrollSelection
	if runtime.GOOS == "linux" {
		if request.reuse != nil {
			// Wait for the converted screenshot editor's close animation.
			if err := waitForCapture(ctx, 250*time.Millisecond); err != nil {
				return nil, err
			}
		} else {
			var err error
			selected, err = v.pickDesktopScrollRegion(ctx, request.displays)
			if err != nil {
				return nil, err
			}
			if selected != nil {
				request.display = selected.display
				b := request.display.Bounds
				request.options.DisplayBounds = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
			}
		}
	}
	log.Printf(brand.Name+": long screenshot requested display: id=%d bounds=%v", request.display.ID, request.options.DisplayBounds)
	source, err := capture.OpenStream(ctx, request.options)
	fallback := false
	if request.options.Automatic && errors.Is(err, capture.ErrAutomaticUnavailable) && ctx.Err() == nil {
		request.options.Automatic, fallback = false, true
		v.later(func() {
			v.notice = "桌面未提供可用的自动采集，正在切换手动长截图，请选择共享来源。"
		})
		source, err = capture.OpenStream(ctx, request.options)
	}
	if err != nil {
		return nil, err
	}
	prepared := &scrollCapture{Stream: source, automatic: request.options.Automatic, fallback: fallback}
	ready := false
	defer func() {
		if !ready {
			prepared.Close()
		}
	}()
	v.later(func() { v.display = request.display; v.notice = "正在连接共享画面…"; v.hideAll() })
	firstCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	first, err := source.Next(firstCtx)
	cancel()
	if err != nil {
		prepared.Close()
		return nil, capture.StreamDiagnostic(source, err)
	}
	log.Printf(brand.Name+": long screenshot connected: %dx%d", first.Rect.Dx(), first.Rect.Dy())
	if runtime.GOOS == "linux" {
		// Let the portal chooser disappear before the selection frame.
		if err := waitForCapture(ctx, 180*time.Millisecond); err != nil {
			return nil, err
		}
		freshCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
		if fresh, err := source.Next(freshCtx); err == nil {
			first = fresh
		}
		cancel()
	}
	prepared.screen = displayForStream(source, request.display, request.displays)
	prepared.size = first.Bounds().Size()
	screen := prepared.screen
	switch {
	case request.reuse != nil && screen != nil && screen.Bounds == request.reuse.Display:
		prepared.region, err = request.reuse.ForStream(prepared.size)
		log.Printf(brand.Name+": long screenshot reused selection: display=%v region=%v", screen.Bounds, prepared.region)
	case selected != nil && screen != nil && screen.Bounds == selected.display.Bounds:
		prepared.region, err = editor.DesktopRegionToStream(selected.region, screen.Bounds, request.displays, selected.desktopSize, prepared.size)
	case screen != nil:
		prepared.region, _, err = editor.PickScreenRegion(ctx, first, []mygo.Display{*screen}, "", image.Pt(80, 160), "框选滚动内容，松开开始 · 避开滚动条和动画 · Esc 取消")
	default:
		prepared.region, err = editor.SelectScrollRegion(ctx, first)
	}
	if err != nil {
		return nil, err
	}
	if prepared.region.Dx() < 80 || prepared.region.Dy() < 160 {
		return nil, errors.New("长截图选区至少需要 80 × 160 像素，请扩大滚动区域")
	}
	if runtime.GOOS == "linux" {
		if err := waitForCapture(ctx, 250*time.Millisecond); err != nil {
			return nil, err
		}
	}
	// Keep the known-clean frame from before the picker; a newly received
	// frame can contain a fading GNOME selection window.
	prepared.baseline = first.SubImage(prepared.region).(*image.RGBA)
	if selected != nil && screen != nil && screen.Bounds == selected.display.Bounds && selected.pixels.Bounds().Size() == prepared.region.Size() {
		prepared.baseline = selected.pixels
	}
	ready = true
	return prepared, nil
}
