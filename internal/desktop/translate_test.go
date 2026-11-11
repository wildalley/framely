package desktop

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/ocr"
	"github.com/wildalley/framely/internal/translate"
)

// translatingWindow is a text window whose translations run at once on the
// test's goroutine, through a stub service.
func translatingWindow(provider string, translator func(context.Context, string, string) (translate.Result, error)) (*ocrWindow, *string) {
	v := newOCRWindow()
	var copied string
	v.copyText = func(text string) error { copied = text; return nil }
	v.provider, v.translator = provider, translator
	v.run, v.later = func(fn func()) { fn() }, func(fn func()) { fn() }
	return v, &copied
}

func TestTextWindowTranslatesOnceTheTextIsIn(t *testing.T) {
	var calls []string
	v, copied := translatingWindow("DeepL", func(_ context.Context, text, target string) (translate.Result, error) {
		calls = append(calls, target+":"+text)
		return translate.Result{Text: "你好，世界", Target: translate.Chinese}, nil
	})
	v.autoTranslate = true
	tt := ui.NewTester(v.view, 760, 620)
	v.finish(ocr.Result{Lines: []ocr.Line{{Text: "Hello, world"}}}, nil)
	tt.Frame()
	if v.tab != 2 || len(calls) != 1 || calls[0] != ":Hello, world" || !tt.HasText("你好，世界") {
		t.Fatalf("tab %d calls %v texts %v", v.tab, calls, tt.Texts())
	}
	if err := tt.Click("复制译文"); err != nil {
		t.Fatal(err)
	}
	if *copied != "你好，世界" {
		t.Fatalf("copied %q", *copied)
	}
	// Choosing a direction translates again into it.
	if err := tt.Click("翻译方向"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("译成英文"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1] != "en:Hello, world" {
		t.Fatalf("calls %v", calls)
	}
}

func TestTextWindowWithoutAServiceSendsNothing(t *testing.T) {
	sent := false
	v, _ := translatingWindow("", func(context.Context, string, string) (translate.Result, error) {
		sent = true
		return translate.Result{}, nil
	})
	opened := false
	v.openSettings = func() { opened = true }
	v.autoTranslate = true
	tt := ui.NewTester(v.view, 760, 620)
	v.finish(ocr.Result{Lines: []ocr.Line{{Text: "Hello"}}}, nil)
	tt.Frame()
	if sent || v.tab != 2 {
		t.Fatalf("sent=%v tab=%d", sent, v.tab)
	}
	if err := tt.Click("设置翻译服务"); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("the settings did not open")
	}
}

func TestTranslationErrorsShowAndClosingDropsLateResults(t *testing.T) {
	v, _ := translatingWindow("百度翻译", func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{}, errors.New("百度翻译：App ID 或密钥无效（52003）")
	})
	v.setText("Hello", false)
	tt := ui.NewTester(v.view, 760, 620)
	v.tab = 2
	v.startTranslate()
	tt.Frame()
	if !tt.HasText("翻译失败：百度翻译：App ID 或密钥无效（52003）") {
		t.Fatalf("texts %v", tt.Texts())
	}
	// A result that arrives after the window closed is dropped.
	var deliver func()
	v.translator = func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{Text: "late"}, nil
	}
	v.later = func(fn func()) { deliver = fn }
	v.startTranslate()
	v.release()
	deliver()
	if v.tr.text != "" {
		t.Fatal("a closed window took a late translation")
	}
}

func TestStoringKeysKeepsWhatIsLeftBlank(t *testing.T) {
	store := map[translate.Provider]credentials{translate.Baidu: {ID: "app", Key: "old"}}
	load, save := loadCredentials, saveCredentials
	loadCredentials = func(p translate.Provider) (credentials, error) { return store[p], nil }
	saveCredentials = func(p translate.Provider, c credentials) error { store[p] = c; return nil }
	defer func() { loadCredentials, saveCredentials = load, save }()
	if err := storeCredentials(translate.Baidu, "", "new"); err != nil {
		t.Fatal(err)
	}
	if got := store[translate.Baidu]; got != (credentials{ID: "app", Key: "new"}) {
		t.Fatalf("stored %+v", got)
	}
}

func TestTranslateSettingsShowTheProvidersFields(t *testing.T) {
	v := &settingsWindow{app: &application{ctx: context.Background()}, ctx: context.Background(), draft: config.Default()}
	v.trStored = map[string]bool{"baidu": true} // Never read the real credential store here.
	v.tab = 4
	tt := ui.NewTester(v.view, 660, 640)
	if r, ok := tt.Find("API Key"); ok && r.W > 0 {
		t.Fatal("a key field shows with no service chosen")
	}
	choose := func(label string) {
		t.Helper()
		if err := tt.Click("选择翻译服务"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	choose("百度翻译")
	for _, label := range []string{"App ID", "密钥", "默认翻译方向", "测试翻译"} {
		if r, ok := tt.Find(label); !ok || r.W <= 0 {
			t.Fatalf("Baidu settings lack %s", label)
		}
	}
	if !tt.HasText("已保存，留空不修改") && v.draft.TranslateProvider != "baidu" {
		t.Fatal("a stored key is not acknowledged")
	}
	choose("大模型（OpenAI 兼容）")
	for _, label := range []string{"API Key", "接口地址", "模型"} {
		if r, ok := tt.Find(label); !ok || r.W <= 0 {
			t.Fatalf("OpenAI-compatible settings lack %s", label)
		}
	}
	if r, ok := tt.Find("App ID"); ok && r.W > 0 {
		t.Fatal("the App ID field stayed for a service without one")
	}
	if v.draft.TranslateProvider != string(translate.OpenAI) {
		t.Fatalf("draft provider %q", v.draft.TranslateProvider)
	}
	saveShortcutPreview(t, tt, "settings-translate")
}

func TestTranslationPreview(t *testing.T) {
	v, _ := translatingWindow("DeepL", func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{Text: "截图之后，让文字自由组合。\nFramely 支持本地识别、选词复制与全文编辑。"}, nil
	})
	v.setText("After a screenshot, let the words combine freely.\nFramely supports local recognition, word picking and full-text editing.", false)
	v.status = "识别完成 · 2 行"
	tt := ui.NewTester(v.view, 760, 620)
	v.tab = 2
	v.startTranslate()
	tt.Frame()
	saveShortcutPreview(t, tt, "ocr-translate")
}

func TestOfflineSettingsListTheModelsWithoutAKey(t *testing.T) {
	v := &settingsWindow{app: &application{ctx: context.Background()}, ctx: context.Background(), draft: config.Default()}
	v.draft.TranslateProvider = string(translate.Offline)
	v.trStored = map[string]bool{}
	// Set the models' states here rather than read the real data folder.
	v.models = map[string]*modelState{"en-zh": {installed: true}, "zh-en": {downloading: true, percent: 42}}
	v.tab = 4
	tt := ui.NewTester(v.view, 660, 640)
	if r, ok := tt.Find("API Key"); ok && r.W > 0 {
		t.Fatal("the offline model asks for a key")
	}
	for _, label := range []string{"删除", "取消", "下载中 42%"} {
		if !tt.HasText(label) {
			if r, ok := tt.Find(label); !ok || r.W <= 0 {
				t.Fatalf("offline settings lack %q: %v", label, tt.Texts())
			}
		}
	}
	v.models["zh-en"] = &modelState{}
	tt.Frame()
	if r, ok := tt.Find("下载"); !ok || r.W <= 0 {
		t.Fatal("a missing model offers no download")
	}
	saveShortcutPreview(t, tt, "settings-translate-offline")
}

func TestOfflineTranslationAsksForTheMissingModel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the models folder follows XDG_DATA_HOME on Linux")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	_, err := translateOffline(context.Background(), "en-zh", []string{"Hello"})
	if err == nil || !strings.Contains(err.Error(), "下载「英 → 中」") {
		t.Fatal(err)
	}
}
