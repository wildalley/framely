package editor

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/wildalley/screenpin/internal/doc"
)

// readDocument decodes files off the UI thread; only fully validated files
// reach the editor. It keeps old .screenpin projects compatible.
func readDocument(path string) (loaded, error) {
	data, err := readLimited(path)
	if err != nil {
		return loaded{}, err
	}
	if strings.EqualFold(filepath.Ext(path), ".screenpin") {
		project, im, err := doc.DecodeProject(data)
		if err != nil {
			return loaded{}, err
		}
		l, err := decode(im)
		if err != nil {
			return loaded{}, err
		}
		l.title, l.createdAt, l.list = project.Title, project.CreatedAt, project.Annotations
		return l, nil
	}
	im, err := doc.DecodeImage(data, filepath.Base(path))
	if err != nil {
		return loaded{}, err
	}
	l, err := decode(im)
	if err != nil {
		return loaded{}, err
	}
	l.title = strings.TrimSuffix(im.Name, filepath.Ext(im.Name))
	l.createdAt = doc.Timestamp(time.Now())
	return l, nil
}

// OpenPath handles a file selected by a dialog, command-line launch or the
// operating system. Call it on the UI thread, like the other editor actions.
func (a *App) OpenPath(path string) {
	if a.busy || a.capturing || a.quitting || a.quitPending {
		return
	}
	a.finishEditing()
	a.panel, a.busy = PanelNone, true
	dirty := a.dirty()
	go func() {
		defer a.later(func() { a.busy = false })
		l, err := readDocument(path)
		if err != nil {
			a.later(func() { a.Show(); a.toast("打开文件失败: " + err.Error()) })
			return
		}
		if dirty {
			a.later(a.Show)
		}
		if !a.confirmDiscard(dirty) {
			return
		}
		a.later(func() {
			a.target = nil
			a.show(l)
			a.Show()
		})
	}()
}
