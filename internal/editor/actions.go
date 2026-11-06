package editor

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

// maxProjectSize keeps a broken or hostile file from filling memory.
const maxProjectSize = 220 << 20

// Capture hides the editor, lets the user pick a region of the screen and
// opens it. Call it on the main thread.
func (a *App) Capture() {
	if a.capturing || a.busy || a.quitPending || a.quitting {
		return
	}
	a.finishEditing()
	a.capturing, a.busy = true, true
	a.panel = PanelNone
	dirty := a.dirty()
	wasVisible := a.win.IsVisible()
	go func() {
		defer a.later(func() {
			a.capturing, a.busy = false, false
			a.activationToken = ""
		})
		if !a.confirmDiscard(dirty) {
			return
		}
		if runtime.GOOS == "linux" {
			a.ensureScreenshotAccess()
		}
		a.win.Hide()
		time.Sleep(180 * time.Millisecond)
		data, target, err := a.grab()
		if err == nil {
			a.later(func() { a.target = target })
			err = a.loadImage(data, captureName())
		}
		switch {
		case err == nil:
			a.later(a.Show)
		case errors.Is(err, capture.ErrCancelled):
			// Backing out puts things back: started from the tray with
			// the editor hidden, it stays hidden.
			if wasVisible {
				a.later(a.Show)
			}
		default:
			a.later(func() {
				a.Show()
				a.toast(err.Error())
			})
		}
	}()
}

// CaptureWithActivation carries a Wayland portal token to the first window
// shown by this capture, including the permission window or region picker.
func (a *App) CaptureWithActivation(token string) {
	if a.capturing || a.busy || a.quitPending || a.quitting {
		return
	}
	a.activationToken = token
	a.Capture()
}

// grab takes the screenshot, off the main thread. On Linux the portal
// captures the whole desktop silently and ScreenPin's own overlays pick the
// region; if either fails, the portal's own selector does.
func (a *App) grab() ([]byte, *mygo.Rectangle, error) {
	if runtime.GOOS != "linux" {
		data, err := capture.System(mygo.Clipboard.ReadImage)
		return data, nil, err
	}
	full, err := capture.Screenshot(false)
	if err == nil {
		data, target, err := a.pickRegion(full)
		if err == nil || errors.Is(err, capture.ErrCancelled) {
			return data, target, err
		}
		log.Printf("ScreenPin: region picker failed (%v); using the portal selector", err)
	} else {
		log.Printf("ScreenPin: full-screen capture failed (%v); using the portal selector", err)
	}
	data, err := capture.Screenshot(true)
	return data, nil, err
}

// ensureScreenshotAccess asks once for silent screenshots. GNOME shows that
// question only to the focused app, and a capture hides the editor first,
// so the first silent request would be refused without asking: while
// nothing is recorded, ask with the window up. The image is thrown away.
func (a *App) ensureScreenshotAccess() {
	if capture.ScreenshotAccess() != capture.AccessUnknown {
		return
	}
	a.later(a.Show)
	time.Sleep(250 * time.Millisecond)
	if _, err := capture.Screenshot(false); err != nil {
		log.Printf("ScreenPin: screenshot permission was not granted: %v", err)
	}
}

// Paste opens the clipboard's image.
func (a *App) Paste() {
	if a.busy || a.capturing || a.quitPending || a.quitting {
		return
	}
	a.finishEditing()
	a.panel, a.busy = PanelNone, true
	dirty := a.dirty()
	go func() {
		defer a.later(func() { a.busy = false })
		if !a.confirmDiscard(dirty) {
			return
		}
		data := mygo.Clipboard.ReadImage()
		if len(data) == 0 {
			a.later(func() { a.toast("剪贴板中没有图片") })
			return
		}
		if err := a.loadImage(data, "剪贴板图片"); err != nil {
			a.later(func() { a.toast(err.Error()) })
		}
	}()
}

// OpenImage opens an image file.
func (a *App) OpenImage() {
	a.openDialog("打开图片", []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "webp"}}})
}

// OpenProject opens a .screenpin project.
func (a *App) OpenProject() {
	a.openDialog("打开工程", []mygo.FileFilter{{Name: "ScreenPin 工程", Extensions: []string{"screenpin"}}})
}

// Dialogs and OS file-open events share the same validation and replacement
// path, and prevent another open while the user is choosing a file.
func (a *App) openDialog(title string, filters []mygo.FileFilter) {
	if a.busy || a.capturing || a.quitPending || a.quitting {
		return
	}
	a.finishEditing()
	a.panel, a.busy = PanelNone, true
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win, Title: title, Filters: filters,
		})
		a.later(func() {
			a.busy = false
			if err != nil {
				a.toast("打开文件对话框失败: " + err.Error())
			} else if len(paths) != 0 {
				a.OpenPath(paths[0])
			}
		})
	}()
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProjectSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProjectSize {
		return nil, errors.New("文件过大")
	}
	return data, nil
}

func withExtension(path, ext string) string {
	if strings.EqualFold(filepath.Ext(path), "."+ext) {
		return path
	}
	return path + "." + ext
}

// SaveProject saves the editable project.
func (a *App) SaveProject() {
	a.finishEditing()
	a.panel = PanelNone
	if a.image == nil {
		a.toast("还没有可保存的工程")
		return
	}
	title, createdAt, im, list := a.title, a.createdAt, *a.image, annotationSnapshot(a.history.Present())
	fingerprint := doc.Fingerprint(title, createdAt, a.image, list)
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      a.win,
			Title:       "保存工程",
			DefaultPath: safeFileName(title) + ".screenpin",
			Filters:     []mygo.FileFilter{{Name: "ScreenPin 工程", Extensions: []string{"screenpin"}}},
		})
		if err != nil || path == "" {
			return
		}
		data, err := doc.EncodeProject(title, createdAt, im, list, time.Now())
		if err == nil {
			err = os.WriteFile(withExtension(path, "screenpin"), data, 0o644)
		}
		a.later(func() {
			if err != nil {
				a.toast("保存工程失败: " + err.Error())
				return
			}
			a.saved = fingerprint
			a.toast("可编辑工程已保存")
		})
	}()
}

// flatten renders the document off the main thread; what it reads is
// copied on the main thread first.
func (a *App) snapshot() func() ([]byte, error) {
	px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
	return func() ([]byte, error) {
		img, err := render.Flatten(px, mosaic, list)
		if err != nil {
			return nil, err
		}
		return render.Encode(img, render.PNG, 1)
	}
}

// Export saves the image with its annotations in the export format.
func (a *App) Export() {
	a.finishEditing()
	a.panel = PanelNone
	if a.image == nil {
		a.toast("还没有可导出的截图")
		return
	}
	px, mosaic, list := a.pixels, a.mosaicPx, annotationSnapshot(a.history.Present())
	format, quality, title := a.exportFormat, a.exportQuality, a.title
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      a.win,
			Title:       "导出图片",
			DefaultPath: safeFileName(title) + "." + format.Extension(),
			Filters:     []mygo.FileFilter{{Name: format.Label() + " 图片", Extensions: []string{format.Extension()}}},
		})
		if err != nil || path == "" {
			return
		}
		img, err := render.Flatten(px, mosaic, list)
		var data []byte
		if err == nil {
			data, err = render.Encode(img, format, quality)
		}
		if err == nil {
			err = os.WriteFile(withExtension(path, format.Extension()), data, 0o644)
		}
		a.later(func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			a.toast(format.Label() + " 已保存")
		})
	}()
}

// Copy puts the image with its annotations on the clipboard.
func (a *App) Copy() {
	a.finishEditing()
	if a.image == nil {
		a.toast("还没有可导出的截图")
		return
	}
	render := a.snapshot()
	go func() {
		data, err := render()
		if err == nil {
			err = mygo.Clipboard.WriteImage(data)
		}
		a.later(func() {
			if err != nil {
				a.toast("复制图片失败: " + err.Error())
				return
			}
			// A pinned window is kept on screen on purpose.
			if a.cfg.HideAfterCopy && !a.pinned && a.trayAvailable {
				a.hide()
				return
			}
			a.toast("已复制到剪贴板")
		})
	}()
}

// TogglePin keeps the window above the others, or stops.
func (a *App) TogglePin() {
	if a.image == nil {
		return
	}
	a.pinned = !a.pinned
	a.win.SetAlwaysOnTop(a.pinned)
	if a.pinned {
		a.toast("已置顶，可继续编辑")
	} else {
		a.toast("已取消置顶")
	}
}

// Close stays reachable even on desktops where creating a tray failed.
func (a *App) Close() {
	if !a.trayAvailable {
		a.RequestQuit()
		return
	}
	a.hide()
}

// A live freehand gesture can mutate its points: exports need their own copy.
func annotationSnapshot(list []doc.Annotation) []doc.Annotation {
	copy := make([]doc.Annotation, len(list))
	for i := range list {
		copy[i] = list[i].Clone()
	}
	return copy
}
