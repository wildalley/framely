package desktop

import (
	"context"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type gifFramePreview struct {
	bitmap        *ui.Bitmap
	at, requested float64
	busy          bool
	err           string
	generation    uint64
	cancel        context.CancelFunc
	load          func(context.Context, string, float64) (image.Image, error)
	run, later    func(func())
}

func (v *videoWindow) cancelGIFPreview() {
	if v.gifFrame.cancel != nil {
		v.gifFrame.cancel()
	}
	v.gifFrame.generation++
	v.gifFrame.cancel, v.gifFrame.busy = nil, false
}

func (v *videoWindow) requestGIFPreview(at float64) {
	f := &v.gifFrame
	if v.closed || v.ctx.Err() != nil || v.gifSource == "" || f.load == nil || f.run == nil || f.later == nil {
		return
	}
	if v.gifLength > 0 {
		at = min(at, max(0, v.gifLength-.05))
	}
	v.cancelGIFPreview()
	ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
	f.cancel, f.busy, f.err, f.requested = cancel, true, "", at
	generation, source, load, later := f.generation, v.gifSource, f.load, f.later
	f.run(func() {
		defer cancel()
		// Canceling this debounce also cancels an in-flight media process.
		timer := time.NewTimer(180 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return
			}
		case <-timer.C:
		}
		var img image.Image
		err := ctx.Err()
		if err == nil {
			img, err = load(ctx, source, at)
		}
		later(func() {
			if v.closed || v.ctx.Err() != nil || generation != v.gifFrame.generation || source != v.gifSource {
				return
			}
			v.gifFrame.busy, v.gifFrame.cancel = false, nil
			if err != nil || img == nil {
				v.gifFrame.err = "无法预览此处，可重试或调整时间；仍可导出选段。"
				return
			}
			v.gifFrame.bitmap, v.gifFrame.at = ui.NewBitmap(img), at
		})
	})
}

func (v *videoWindow) gifRange() (start, duration float64, valid bool) {
	v.gifErrors = map[string]string{}
	start, e1 := strconv.ParseFloat(strings.TrimSpace(v.gifStart), 64)
	duration, e2 := strconv.ParseFloat(strings.TrimSpace(v.gifDuration), 64)
	if e1 != nil || math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start > 86400 {
		v.gifErrors["start"] = "请输入 0–86400 之间的开始秒数。"
	}
	if e2 != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 60 {
		v.gifErrors["duration"] = "时长必须大于 0，且最多 60 秒。"
	}
	if v.gifLength > 0 && v.gifErrors["start"] == "" {
		if start >= v.gifLength {
			v.gifErrors["start"] = fmt.Sprintf("开始时间须小于视频长度（%.2f 秒）。", v.gifLength)
		} else {
			duration = min(duration, v.gifLength-start)
		}
	}
	return start, duration, len(v.gifErrors) == 0
}

func (v *videoWindow) syncGIFRange(startChanged bool) {
	start, duration, valid := v.gifRange()
	if !valid {
		v.cancelGIFPreview()
		return
	}
	v.gifLow, v.gifHigh = start, start+duration
	if requested, _ := strconv.ParseFloat(v.gifDuration, 64); requested > duration {
		v.gifDuration = strconv.FormatFloat(duration, 'f', 3, 64)
	}
	at := start
	if !startChanged {
		at = max(start, v.gifHigh-.05)
	}
	v.requestGIFPreview(at)
}

func (v *videoWindow) gifFieldError(c *ui.Context, field string) {
	if message := v.gifErrors[field]; message != "" {
		ui.Text(c, message).FontSize(12).TextColor(c.Theme().Danger).FillWidth()
	}
}

func (v *videoWindow) gifSelectionView(c *ui.Context) {
	f := &v.gifFrame
	ui.Column(c.Key("gif-selection")).FillWidth().Gap(6).Children(func() {
		if f.bitmap != nil {
			ui.Image(c, f.bitmap).Fit(ui.Contain).FillWidth().Height(126).Label("GIF 选段画面预览")
		} else {
			ui.Box(c).FillWidth().Height(126).Radius(8).Background(c.Theme().Surface).Center().Children(func() {
				ui.Text(c, "选段画面预览").FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}
		caption := fmt.Sprintf("预览 %.2f 秒", f.at)
		if f.bitmap == nil {
			caption = "拖动起止点或输入秒数来定位画面"
		}
		if f.busy {
			caption += fmt.Sprintf(" · 正在定位 %.2f 秒…", f.requested)
		}
		ui.Text(c, caption).FontSize(12).TextColor(c.Theme().TextMuted)
		if f.err != "" {
			ui.Text(c, f.err).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c.Key("gif-preview-retry"), "重试预览").Clicked() {
				v.requestGIFPreview(f.requested)
			}
		}
		if v.gifLength <= 0 {
			ui.Text(c, "未能读取视频时长，请使用秒数输入选段。").FontSize(12).TextColor(c.Theme().TextMuted)
			return
		}
		low, high := v.gifLow, v.gifHigh
		if ui.RangeSlider(c.Key("gif-timeline"), &v.gifLow, &v.gifHigh, 0, v.gifLength, 0).FillWidth().Label("GIF 选段").Disabled(v.exporting).Changed() {
			minimum := min(.05, v.gifLength)
			if v.gifLow != low {
				v.gifLow = min(v.gifLow, v.gifLength-minimum)
				v.gifHigh = min(v.gifLength, max(v.gifLow+minimum, min(v.gifHigh, v.gifLow+60)))
			} else {
				v.gifHigh = max(v.gifHigh, minimum)
				v.gifLow = max(0, min(v.gifHigh-minimum, max(v.gifLow, v.gifHigh-60)))
			}
			v.gifStart = strconv.FormatFloat(v.gifLow, 'f', 3, 64)
			v.gifDuration = strconv.FormatFloat(v.gifHigh-v.gifLow, 'f', 3, 64)
			v.syncGIFRange(v.gifLow != low || v.gifHigh == high)
		}
		ui.Text(c, fmt.Sprintf("选段 %.2f–%.2f 秒 · %.2f 秒（最多 60 秒）", v.gifLow, v.gifHigh, v.gifHigh-v.gifLow)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}
