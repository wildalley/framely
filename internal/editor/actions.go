package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/wildalley/framely/internal/doc"
	"github.com/wildalley/framely/internal/render"
)

func withExtension(path, ext string) string {
	if strings.EqualFold(filepath.Ext(path), "."+ext) {
		return path
	}
	return path + "." + ext
}

// Workers take an independent annotation snapshot on the main thread. The
// pixels are immutable for the whole lifetime of this particular image.
func (a *App) snapshot() func() ([]byte, error) {
	px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
	ratio := a.history.ImageCornerRatio()
	ctx := a.ctx
	lease := a.lease.Retain()
	return func() ([]byte, error) {
		defer lease.Release()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		img, err := render.FlattenWithCorners(px, mosaic, list, ratio)
		if err != nil {
			return nil, err
		}
		return render.EncodeContext(ctx, img, render.PNG, 1)
	}
}

// Export freezes input while choosing a destination, then snapshots the image.
// A dismissed or orphaned dialog keeps no pixels or annotations alive.
func (a *App) Export() {
	if a.closed || a.busy || a.image == nil {
		return
	}
	a.finishEditing()
	a.panel, a.busy = PanelNone, true
	format, quality, title, parent, ctx := a.exportFormat, a.exportQuality, a.title, a.win, a.ctx
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: parent, Title: "保存图片", ButtonLabel: "保存",
			DefaultPath: safeFileName(title) + "." + format.Extension(),
			Filters:     []mygo.FileFilter{{Name: format.Label() + " 图片", Extensions: []string{format.Extension()}}},
		})
		a.later(func() {
			if err != nil || path == "" {
				a.busy = false
				if err != nil {
					a.toast("保存图片失败: " + err.Error())
				}
				return
			}
			px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
			ratio := a.history.ImageCornerRatio()
			lease := a.lease.Retain()
			go func() {
				defer lease.Release()
				if ctx.Err() != nil {
					return
				}
				img, err := render.FlattenWithCorners(px, mosaic, list, ratio)
				var data []byte
				if err == nil {
					data, err = render.EncodeContext(ctx, img, format, quality)
				}
				if err == nil {
					err = writeImage(ctx, withExtension(path, format.Extension()), data)
				}
				a.later(func() {
					a.busy = false
					if err != nil {
						a.toast("保存图片失败: " + err.Error())
						return
					}
					a.toast(format.Label() + " 已保存")
				})
			}()
		})
	}()
}

// writeImage checks cancellation before writing; closing the image cancels
// work whose destination dialog has not completed yet.
func writeImage(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (a *App) Copy() {
	if a.closed || a.busy || a.image == nil {
		return
	}
	a.finishEditing()
	a.busy = true
	makePNG := a.snapshot()
	go func() {
		data, err := makePNG()
		a.later(func() {
			// Dispatch the clipboard write only if this image still exists.
			a.busy = false
			if err == nil {
				err = mygo.Clipboard.WriteImage(data)
			}
			if err != nil {
				a.toast("复制图片失败: " + err.Error())
				return
			}
			if a.cfg.HideAfterCopy && !a.pinned {
				a.Close()
				return
			}
			a.toast("已复制到剪贴板")
		})
	}()
}

func (a *App) TogglePin() {
	if a.closed || a.win == nil {
		return
	}
	a.pinned = !a.pinned
	a.win.SetAlwaysOnTop(a.pinned)
}

func annotationSnapshot(list []doc.Annotation) []doc.Annotation {
	copy := make([]doc.Annotation, len(list))
	for i := range list {
		copy[i] = list[i].Clone()
	}
	return copy
}
