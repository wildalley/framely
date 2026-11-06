// Package desktop connects the native editor to the application lifecycle,
// tray, single-instance handoff, global shortcuts and system login items.
package desktop

import (
	"context"
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/assets"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/editor"
	"github.com/wildalley/screenpin/internal/shortcuts"
)

type application struct {
	ctx       context.Context
	cancel    context.CancelFunc
	editor    *editor.App
	shortcuts *shortcuts.Manager
	tray      *mygo.Tray
	menu      *mygo.Menu
	capture   *mygo.MenuItem
	autostart *mygo.MenuItem
}

// Run starts the event loop on the main goroutine. Every callback touching
// editor state runs on the UI thread; permission dialogs wait in goroutines.
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
	mygo.Theme.SetSource(mygo.ThemeLight)
	mygo.App.WhenReady(func() { s.ready(launch) })
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		next, err := ParseArgs(args)
		if err == nil && s.editor != nil {
			if next.File != "" && !filepath.IsAbs(next.File) {
				next.File = filepath.Join(workingDir, next.File)
			}
			s.launch(next)
		}
	})
	mygo.App.OnOpenFile(func(path string) {
		if s.editor != nil {
			s.editor.OpenPath(path)
		}
	})
	mygo.App.OnActivate(func(bool) {
		if s.editor != nil {
			s.editor.Show()
		}
	})
	mygo.App.OnBeforeQuit(func(event *mygo.QuitEvent) {
		if s.editor != nil && !s.editor.IsQuitting() {
			event.PreventDefault()
			s.editor.RequestQuit()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		s.cancel()
		// Native registrations dispatch to this thread. Do not wait here
		// for a worker holding the manager lock while dispatching to us.
		mygo.GlobalShortcut.UnregisterAll()
		if s.tray != nil {
			s.tray.Destroy()
		}
	})
	err := mygo.App.Run()
	s.cancel()
	if s.shortcuts != nil {
		s.shortcuts.Close()
	}
	capture.Close()
	return err
}

func (s *application) ready(launch Launch) {
	cfg, repaired := config.Load()
	s.editor = editor.New(cfg)
	s.editor.Open(launch.Hidden || mygo.App.WasOpenedAtLogin() || launch.File != "")
	s.editor.SetShortcutDescription("注册中…")
	s.shortcuts = shortcuts.New(s.editor.CaptureWithActivation)
	s.editor.OnSaveSettings = func(next config.Config) error {
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		defer cancel()
		return s.shortcuts.Apply(ctx, next)
	}
	s.editor.OnSettingsChanged = func(config.Config) {
		s.editor.SetShortcutDescription(s.shortcuts.Description())
		s.refreshTray()
	}
	s.editor.OnAutostartChanged = func(on bool) {
		if s.autostart != nil {
			s.autostart.SetChecked(on)
		}
	}
	s.createTray()
	mygo.App.OnDidBecomeActive(func() {
		on := mygo.App.OpenAtLogin()
		s.editor.AutostartChanged(on)
		if s.autostart != nil {
			s.autostart.SetChecked(on)
		}
	})
	go func() {
		if repaired {
			if err := cfg.Save(); err != nil {
				log.Printf("ScreenPin: could not save repaired configuration: %v", err)
			}
		}
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		defer cancel()
		err := s.shortcuts.Start(ctx, cfg)
		description := s.shortcuts.Description()
		s.editor.Window().Update(func() {
			if err != nil && description == "" {
				log.Printf("ScreenPin: global shortcut: %v", err)
				s.editor.SetShortcutDescription("未绑定")
				s.editor.Notify("全局快捷键注册失败；可通过托盘或 screenpin --capture 截图。" + err.Error())
			} else {
				s.editor.SetShortcutDescription(description)
			}
			s.refreshTray()
		})
	}()
	if launch.Capture {
		s.editor.Capture()
	} else if launch.File != "" {
		// MyGo only forwards registered file extensions; image arguments
		// and unpackaged go run builds must also open explicitly.
		s.editor.OpenPath(launch.File)
	}
}

func (s *application) launch(launch Launch) {
	switch {
	case launch.Capture:
		s.editor.Capture()
	case launch.File != "":
		s.editor.Show()
		s.editor.OpenPath(launch.File)
	case launch.Hidden:
	default:
		s.editor.Show()
	}
}

func (s *application) createTray() {
	s.capture = &mygo.MenuItem{ID: "capture", Label: "截取屏幕", Click: func(*mygo.MenuItem, *mygo.Window) { s.editor.Capture() }}
	s.autostart = &mygo.MenuItem{ID: "autostart", Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(),
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			if err := mygo.App.SetOpenAtLogin(item.IsChecked()); err != nil {
				s.editor.Notify(err.Error())
			}
			on := mygo.App.OpenAtLogin()
			item.SetChecked(on)
			s.editor.AutostartChanged(on)
		}}
	s.menu = mygo.NewMenu([]*mygo.MenuItem{
		s.capture,
		{Label: "显示编辑器", Click: func(*mygo.MenuItem, *mygo.Window) { s.editor.Show() }},
		{Label: "打开图片", Click: func(*mygo.MenuItem, *mygo.Window) { s.editor.Show(); s.editor.OpenImage() }},
		{Label: "打开工程", Click: func(*mygo.MenuItem, *mygo.Window) { s.editor.Show(); s.editor.OpenProject() }},
		mygo.Separator(), s.autostart, mygo.Separator(),
		{Label: "退出 ScreenPin", Click: func(*mygo.MenuItem, *mygo.Window) { s.editor.RequestQuit() }},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: capture.AppID, Icon: assets.TrayIcon, ToolTip: "ScreenPin", Menu: s.menu})
	s.tray = tray
	s.editor.SetTrayAvailable(err == nil && tray != nil)
	if err != nil {
		log.Printf("ScreenPin: tray unavailable: %v", err)
		s.editor.Show()
		s.editor.Notify("系统托盘不可用，关闭窗口将退出 ScreenPin。")
		return
	}
	s.refreshTray()
}

func (s *application) refreshTray() {
	if s.tray == nil {
		return
	}
	s.capture.SetLabel("截取屏幕（" + s.editor.ShortcutDescription() + "）")
	s.tray.SetMenu(s.menu)
}
