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
	if a.closed || a.busy || a.ocrRunning || a.image == nil || a.OnOCRResult == nil {
		return
	}
	a.finishEditing()
	ctx, cancel := context.WithCancel(a.ctx)
	a.ocrCancel = cancel
	a.ocrRunning = true
	px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
	ratio := a.history.ImageCornerRatio()
	a.toast("正在离线识别文字，关闭图片会取消。")
	go func() {
		defer cancel()
		img, err := render.FlattenWithCorners(px, mosaic, list, ratio)
		if err == nil && region {
			var choiceErr error
			r, choiceErr := SelectImageRegion(ctx, img, "框选识别区域")
			err = choiceErr
			if err == nil {
				img = img.SubImage(r.Add(img.Rect.Min)).(*image.RGBA)
			}
		}
		var result ocr.Result
		if err == nil {
			result, err = ocr.Recognize(ctx, img)
		}
		a.later(func() {
			a.ocrRunning = false
			a.ocrCancel = nil
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
			a.OnOCRResult(result)
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
