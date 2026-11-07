// Package desktop keeps the tray, shortcuts and settings alive independently
// of ScreenPin's temporary image windows.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/assets"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/editor"
	"github.com/wildalley/screenpin/internal/niribind"
	"github.com/wildalley/screenpin/internal/portal"
	"github.com/wildalley/screenpin/internal/shortcuts"
)

type application struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	workers             sync.WaitGroup
	cfg                 config.Config
	readyDone           bool
	pendingFiles        []string
	images              []*editor.App
	settings            *settingsWindow
	shortcuts           *shortcuts.Manager
	shortcutDescription string
	shortcutError       string
	shortcutPending     bool
	shortcutGeneration  int
	tray                *mygo.Tray
	menu                *mygo.Menu
	captureItem         *mygo.MenuItem
	captureBusy         bool
	openingDialog       bool
	scroll              *scrollWindow
	operations          int
}

func Run(launch Launch) error {
	mygo.App.SetName("ScreenPin")
	mygo.App.SetVersion(Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &application{ctx: ctx, cancel: cancel}
	if runtime.GOOS == "darwin" {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}}))
	} else {
		mygo.App.SetMenu(nil)
	}
	mygo.Theme.SetSource(mygo.ThemeSystem)
	mygo.App.WhenReady(func() { s.ready(launch) })
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		next, err := ParseArgs(args)
		if err != nil {
			return
		}
		if next.File != "" && !filepath.IsAbs(next.File) {
			next.File = filepath.Join(workingDir, next.File)
		}
		s.launch(next)
	})
	mygo.App.OnOpenFile(func(path string) {
		if !s.readyDone {
			s.pendingFiles = append(s.pendingFiles, path)
			return
		}
		s.OpenPath(path)
	})
	mygo.App.OnActivate(func(bool) {
		if len(s.images) > 0 {
			s.images[len(s.images)-1].Show()
		}
	})
	// MyGo normally quits on the last window. A tray utility instead keeps
	// running without any image or hidden home window.
	mygo.App.OnWindowAllClosed(s.maybeQuit)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.cancel() })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		s.cancel()
		// Do not wait for a manager lock on the UI thread: its worker may be
		// registering a native shortcut by dispatching to this very thread.
		mygo.GlobalShortcut.UnregisterAll()
		if s.tray != nil {
			s.tray.Destroy()
			s.tray = nil
		}
	})
	err := mygo.App.Run()
	s.cancel()
	if s.shortcuts != nil {
		s.shortcuts.Close()
	}
	s.workers.Wait()
	capture.Close()
	return err
}

func (s *application) worker(fn func()) {
	s.workers.Add(1)
	go func() { defer s.workers.Done(); fn() }()
}

func (s *application) dispatch(fn func()) {
	mygo.RunOnMain(func() {
		if s.ctx.Err() != nil {
			return
		}
		fn()
		if s.settings != nil {
			s.settings.win.Invalidate()
		}
	})
}

func (s *application) ready(launch Launch) {
	cfg, repaired := config.Load()
	s.cfg, s.readyDone = cfg, true
	s.shortcutDescription = "注册中…"
	s.shortcutPending = true
	s.shortcuts = shortcuts.New(s.CaptureWithActivation)
	s.createTray()
	mygo.App.OnDidBecomeActive(func() {
		if s.ctx.Err() != nil {
			return
		}
		on := mygo.App.OpenAtLogin()
		if s.settings != nil {
			s.settings.autostart = on
			s.settings.win.Invalidate()
		}
	})
	s.worker(func() {
		if repaired {
			if err := cfg.Save(); err != nil {
				log.Printf("ScreenPin: repaired configuration: %v", err)
			}
		}
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		defer cancel()
		err := s.shortcuts.Start(ctx, cfg)
		description := s.shortcuts.Description()
		s.dispatch(func() {
			if s.shortcutGeneration != 0 {
				return
			}
			s.bindingResult(description, err)
			if err != nil && description == "" && !cancelled(err) && !launch.Hidden {
				s.showSettings("快捷键注册未完成，请在设置中重试。")
				if niribind.Current() {
					s.settings.setupNiri()
				}
			}
		})
	})
	s.launch(launch)
	for _, path := range s.pendingFiles {
		s.OpenPath(path)
	}
	s.pendingFiles = nil
	if s.tray == nil && s.operations == 0 {
		s.showSettings("系统托盘不可用，关闭最后一个窗口将退出 ScreenPin。")
	}
}

func (s *application) launch(launch Launch) {
	if s.ctx.Err() != nil {
		return
	}
	switch {
	case launch.Scroll:
		s.showScroll()
	case launch.Capture:
		s.Capture()
	case launch.File != "":
		s.OpenPath(launch.File)
		// Starting or re-launching without an image leaves the tray alone.
	}
}

func (s *application) maybeQuit() {
	if s.ctx.Err() != nil {
		return
	}
	if s.tray == nil && s.operations == 0 && len(s.images) == 0 && s.settings == nil {
		mygo.App.Quit()
	}
}

func (s *application) showPicture(picture editor.Picture, target *mygo.Rectangle, token string) {
	if s.scroll != nil && s.scroll.running {
		s.scroll.show()
	}
	image := editor.New(s.cfg)
	image.OnPaste = s.Paste
	image.OnCapture = s.Capture
	if capture.ScrollingSupported() {
		image.OnScroll = s.showScroll
	}
	image.OnSettings = func() { s.showSettings("") }
	image.OnOCRResult = s.showOCR
	image.OnClosed = func() {
		if i := slices.Index(s.images, image); i >= 0 {
			s.images = slices.Delete(s.images, i, i+1)
		}
		s.maybeQuit()
	}
	if image.Open(s.ctx, picture, target, token) != nil {
		s.images = append(s.images, image)
	}
}

// beginImage loads in the background and creates a new window only on success.
// All existing image windows keep their own pixels, annotations and history.
func (s *application) beginImage(load func(context.Context) (editor.Picture, *mygo.Rectangle, string, error), done func()) {
	if s.ctx.Err() != nil {
		return
	}
	s.operations++
	s.worker(func() {
		picture, target, token, err := load(s.ctx)
		s.dispatch(func() {
			s.operations--
			if done != nil {
				done()
			}
			if err == nil {
				s.showPicture(picture, target, token)
			} else if !cancelled(err) {
				s.notify(err.Error())
			}
			s.maybeQuit()
		})
	})
}

func cancelled(err error) bool {
	return errors.Is(err, capture.ErrCancelled) || errors.Is(err, portal.ErrCancelled) || errors.Is(err, context.Canceled)
}

func (s *application) OpenPath(path string) {
	if s.blockPictureDuringScroll() {
		return
	}
	s.beginImage(func(ctx context.Context) (editor.Picture, *mygo.Rectangle, string, error) {
		if err := ctx.Err(); err != nil {
			return editor.Picture{}, nil, "", err
		}
		picture, err := editor.ReadPicture(path)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return editor.Picture{}, nil, "", fmt.Errorf("打开图片失败: %w", err)
		}
		return picture, nil, "", nil
	}, nil)
}

func (s *application) OpenImage() {
	if s.blockPictureDuringScroll() {
		return
	}
	if s.ctx.Err() != nil || s.openingDialog {
		return
	}
	s.openingDialog = true
	s.operations++
	s.worker(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: "打开图片", ButtonLabel: "打开",
			Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "webp"}}},
		})
		s.dispatch(func() {
			s.openingDialog = false
			s.operations--
			if err != nil {
				s.notify("打开图片失败: " + err.Error())
			} else if len(paths) > 0 {
				s.OpenPath(paths[0])
			}
			s.maybeQuit()
		})
	})
}

func (s *application) Paste() {
	if s.blockPictureDuringScroll() {
		return
	}
	s.beginImage(func(ctx context.Context) (editor.Picture, *mygo.Rectangle, string, error) {
		if err := ctx.Err(); err != nil {
			return editor.Picture{}, nil, "", err
		}
		data := mygo.Clipboard.ReadImage()
		if len(data) == 0 {
			return editor.Picture{}, nil, "", errors.New("剪贴板中没有图片")
		}
		picture, err := editor.DecodePicture(data, "剪贴板图片")
		return picture, nil, "", err
	}, nil)
}

func (s *application) Capture() { s.CaptureWithActivation("") }

func (s *application) CaptureWithActivation(token string) {
	if s.ctx.Err() != nil {
		return
	}
	if s.settings != nil && s.settings.recording {
		return
	}
	if s.scroll != nil && s.scroll.running {
		s.scroll.finish()
		return
	}
	if s.captureBusy {
		return
	}
	if s.settings != nil && s.settings.asking {
		s.settings.notice = "正在等待截图授权，请先完成系统提示。"
		s.settings.win.Invalidate()
		return
	}
	s.captureBusy = true
	s.refreshTray()
	// Keep ScreenPin's own windows out of a fresh screenshot. These windows
	// are restored after selection; a user-closed image is never restored.
	focused := mygo.FocusedWindow()
	var hidden []*mygo.Window
	for _, win := range mygo.Windows() {
		if win.IsVisible() {
			hidden = append(hidden, win)
			win.Hide()
		}
	}
	s.beginImage(func(ctx context.Context) (editor.Picture, *mygo.Rectangle, string, error) {
		if len(hidden) > 0 {
			timer := time.NewTimer(180 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return editor.Picture{}, nil, "", ctx.Err()
			case <-timer.C:
			}
		}
		data, target, remaining, err := grab(ctx, token)
		if err != nil {
			return editor.Picture{}, nil, "", err
		}
		picture, err := editor.DecodePicture(data, "截图 "+time.Now().Format("15-04-05"))
		return picture, target, remaining, err
	}, func() {
		for _, win := range hidden {
			if !win.IsDestroyed() {
				win.ShowInactive()
			}
		}
		if focused != nil && !focused.IsDestroyed() {
			focused.Focus()
		}
		s.captureBusy = false
		s.refreshTray()
	})
}

// First-use capture uses the system selector without opening a fake editor
// just to focus a permission prompt. Once granted, use ScreenPin's own picker.
func grab(ctx context.Context, token string) ([]byte, *mygo.Rectangle, string, error) {
	if runtime.GOOS != "linux" {
		data, err := capture.SystemContext(ctx, mygo.Clipboard.ReadImage)
		return data, nil, token, err
	}
	if capture.ScreenshotAccessContext(ctx) == capture.AccessGranted {
		full, err := capture.ScreenshotContext(ctx, false)
		if err == nil {
			data, target, pickErr := editor.PickRegion(ctx, full, token)
			// The token activated the picker; it must not be used a second time.
			token = ""
			if pickErr == nil || cancelled(pickErr) || ctx.Err() != nil {
				return data, target, token, pickErr
			}
			log.Printf("ScreenPin: region picker: %v; using system selector", pickErr)
		} else if cancelled(err) || ctx.Err() != nil {
			return nil, nil, token, err
		}
	}
	data, err := capture.ScreenshotContext(ctx, true)
	return data, nil, token, err
}

// trayItems is shared by the native tray and its interaction regression tests.
func (s *application) trayItems() []*mygo.MenuItem {
	s.captureItem = &mygo.MenuItem{ID: "capture", Label: "截图", Click: func(*mygo.MenuItem, *mygo.Window) { s.Capture() }}
	return []*mygo.MenuItem{
		s.captureItem,
		{ID: "scroll-capture", Label: "长截图…", Disabled: !capture.ScrollingSupported(), Click: func(*mygo.MenuItem, *mygo.Window) { s.showScroll() }},
		mygo.Separator(),
		{ID: "paste", Label: "粘贴图片", Click: func(*mygo.MenuItem, *mygo.Window) { s.Paste() }},
		{ID: "open-image", Label: "打开图片…", Click: func(*mygo.MenuItem, *mygo.Window) { s.OpenImage() }},
		mygo.Separator(),
		{ID: "settings", Label: "设置与快捷键…", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings("") }},
		mygo.Separator(),
		{ID: "quit", Label: "退出 ScreenPin", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}
}
func (s *application) createTray() {
	s.menu = mygo.NewMenu(s.trayItems())
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: capture.AppID, Icon: assets.TrayIcon, ToolTip: "ScreenPin", Menu: s.menu})
	if err != nil {
		log.Printf("ScreenPin: tray unavailable: %v", err)
		return
	}
	s.tray = tray
	s.refreshTray()
}

func (s *application) refreshTray() {
	if s.tray == nil {
		return
	}
	label := "截图"
	if !s.shortcutPending && s.shortcutError == "" {
		label += "（" + config.DisplayShortcut(s.cfg.CaptureShortcut) + "）"
	}
	if s.captureBusy {
		label = "正在截图…"
	}
	if s.scroll != nil && s.scroll.running {
		label = "完成长截图"
	}
	s.captureItem.SetLabel(label)
	s.captureItem.SetEnabled(!s.captureBusy || s.scroll != nil && s.scroll.running)
	longActive := s.scroll != nil && (s.scroll.busy || s.scroll.running)
	for _, id := range []string{"paste", "open-image"} {
		s.menu.ItemByID(id).SetEnabled(!longActive)
	}
	scroll := s.menu.ItemByID("scroll-capture")
	scroll.SetEnabled(capture.ScrollingSupported() && (!s.captureBusy || s.scroll != nil))
	scroll.SetLabel("长截图…")
	if longActive {
		scroll.SetLabel("查看长截图…")
	}
	s.tray.SetMenu(s.menu)
}

func (s *application) setAutostart(want bool) {
	if err := mygo.App.SetOpenAtLogin(want); err != nil {
		s.notify("设置开机自启失败: " + err.Error())
	}
	on := mygo.App.OpenAtLogin()
	if s.settings != nil {
		s.settings.autostart = on
		s.settings.win.Invalidate()
	}
}

func (s *application) bindingResult(description string, err error) {
	s.shortcutPending = false
	s.shortcutDescription = description
	if description == "" {
		s.shortcutDescription = "未绑定"
	}
	s.shortcutError = ""
	if err != nil {
		if description == "" {
			s.shortcutError = err.Error()
		}
		log.Printf("ScreenPin: shortcut: %v", err)
	}
	s.refreshTray()
}

func (s *application) saveSettings(ctx context.Context, next config.Config, done func(error)) {
	s.applySettings(ctx, next, s.shortcuts.Apply, done)
}

func (s *application) applySettings(ctx context.Context, next config.Config, apply func(context.Context, config.Config) error, done func(error)) {
	if s.shortcutPending {
		done(errors.New("系统正在处理快捷键授权，请等待当前请求完成"))
		return
	}
	s.shortcutPending = true
	s.shortcutGeneration++
	s.worker(func() {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		err := apply(ctx, next)
		description := s.shortcuts.Description()
		s.dispatch(func() {
			if err == nil {
				s.cfg = next
				for _, image := range s.images {
					image.SetConfig(next)
				}
			}
			s.bindingResult(description, err)
			done(err)
		})
	})
}

// Errors from tray actions use a native dialog; they never manufacture an
// empty image window. Settings errors stay next to their controls.
func (s *application) notify(message string) {
	log.Printf("ScreenPin: %s", message)
	if s.settings != nil {
		s.settings.notice = message
		s.settings.win.Invalidate()
		return
	}
	s.operations++
	s.worker(func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageError, Title: "ScreenPin", Message: message, Buttons: []string{"关闭"}})
		s.dispatch(func() { s.operations--; s.maybeQuit() })
	})
}
