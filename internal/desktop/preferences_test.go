package desktop

import (
	"context"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/config"
)

func TestShortcutAuthorizationIsVisibleAndCannotBeSubmittedTwice(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux portal authorization entry")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	s := &application{ctx: context.Background(), shortcutDescription: "未绑定", shortcutError: "当前桌面未能创建快捷键会话"}
	v := &settingsWindow{app: s, ctx: context.Background(), draft: config.Default()}
	tt := ui.NewTester(v.view, 520, 500)
	if _, ok := tt.Find("授权快捷键"); !ok {
		t.Fatal("a failed shortcut registration has no authorization action")
	}
	s.shortcutPending = true
	tt.Frame()
	for _, label := range []string{"授权快捷键", "保存设置", "录制快捷键"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if v.saving || v.recording {
			t.Fatalf("%s started another request during authorization", label)
		}
	}
	v.save()
	if v.saving {
		t.Fatal("save did not reject an authorization already in progress")
	}
}

func TestNiriUsesAConfigurationConfirmationInsteadOfUnsupportedPortalRetry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("niri integration")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	s := &application{ctx: context.Background(), shortcutDescription: "未绑定", shortcutError: "门户后端不可用", shortcutPending: true}
	v := &settingsWindow{app: s, ctx: context.Background(), draft: config.Default()}
	tt := ui.NewTester(v.view, 520, 600)
	if _, ok := tt.Find("配置 niri 快捷键"); !ok {
		t.Fatal("niri has no configuration action")
	}
	if _, ok := tt.Find("授权快捷键"); ok {
		t.Fatal("niri repeats a portal request instead of offering configuration")
	}
	if err := tt.Click("配置 niri 快捷键"); err != nil {
		t.Fatal(err)
	}
	if v.saving {
		t.Fatal("pending registration allowed another configuration request")
	}
}

func TestSettingsTabsShareDraftAndKeepSaveVisibleAtMinimumSize(t *testing.T) {
	v := &settingsWindow{app: &application{ctx: context.Background()}, ctx: context.Background(), draft: config.Default()}
	tt := ui.NewTester(v.view, 420, 380)
	if err := tt.Click("常规"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("复制后关闭图片"); err != nil {
		t.Fatal(err)
	}
	if !v.draft.HideAfterCopy {
		t.Fatal("general preference was not changed")
	}
	if err := tt.Click("权限与长截图"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("打开长截图"); !ok {
		t.Fatal("capture permissions are not reachable")
	}
	if err := tt.Click("快捷键"); err != nil {
		t.Fatal(err)
	}
	if !v.draft.HideAfterCopy {
		t.Fatal("tab navigation discarded pending settings")
	}
	for _, label := range []string{"保存设置", "关闭"} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 380.5 || r.X+r.W > 420.5 {
			t.Fatalf("%s is obscured: %+v", label, r)
		}
	}
}
