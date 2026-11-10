package editor

import (
	"context"
	"errors"
	"image"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/ocr"
	"github.com/wildalley/screenpin/internal/render"
)

func (a *App) recognize(region bool) {
	if a.closed || a.busy || a.ocrRunning || a.image == nil || (a.OnOCRResult == nil && a.OnOCRStart == nil) {
		return
	}
	a.finishEditing()
	ctx, cancel := context.WithCancel(a.ctx)
	a.ocrCancel = cancel
	a.ocrRunning = true
	px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
	ratio := a.history.ImageCornerRatio()
	lease := a.lease.Retain()
	go func() {
		defer lease.Release()
		defer cancel()
		r := px.Bounds()
		var err error
		if region {
			r, err = SelectOCRRegion(ctx, px, mosaic, list, ratio)
			if err == nil {
				r = r.Add(px.Rect.Min)
			}
		}
		var progress func(ocr.Progress)
		var complete func(ocr.Result, error)
		if err == nil {
			a.later(func() {
				if a.OnOCRStart != nil {
					progress, complete = a.OnOCRStart(cancel)
				}
			})
		}
		var result ocr.Result
		if err == nil {
			result, err = ocr.RecognizePrepared(ctx, func(ctx context.Context) (*image.RGBA, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return render.FlattenRegion(px, mosaic, list, ratio, r)
			}, progress)
		}
		if complete != nil {
			complete(result, err)
		}
		a.later(func() {
			a.ocrRunning = false
			a.ocrCancel = nil
			if complete != nil {
				return // The result window owns progress and error feedback.
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, capture.ErrCancelled) {
					a.toast("识别失败：" + err.Error())
				}
				return
			}
			if len(result.Lines) == 0 {
				a.toast("没有识别到文字，请尝试框选或放大原图。 ")
				return
			}
			if complete == nil && a.OnOCRResult != nil {
				a.OnOCRResult(result)
			}
		})
	}()
}

func (a *App) ocrMenu(m *ui.Menu) {
	if m.Item("识别整张图片").Disabled(a.ocrRunning).Chosen() {
		a.recognize(false)
	}
	if m.Item("框选识别文字").Disabled(a.ocrRunning).Chosen() {
		a.recognize(true)
	}
	if a.ocrRunning && m.Item("取消文字识别").Chosen() {
		if a.ocrCancel != nil {
			a.ocrCancel()
		}
	}
}
