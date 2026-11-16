package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

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
	if !tt.HasText("翻译未完成，请重试或检查服务设置。") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("查看错误详情"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("百度翻译：App ID 或密钥无效（52003）") {
		t.Fatal("diagnostic details missing")
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

func TestCancelAndFailedRetranslationKeepPreviousResult(t *testing.T) {
	v, copied := translatingWindow("DeepL", func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{Text: "旧译文", Target: translate.Chinese}, nil
	})
	v.setText("Hello", false)
	v.tab = 2
	v.startTranslate()
	var work func()
	v.run = func(fn func()) { work = fn }
	var request context.Context
	v.translator = func(ctx context.Context, _, _ string) (translate.Result, error) {
		request = ctx
		return translate.Result{Text: "迟到译文", Target: translate.English}, nil
	}
	v.text, v.target = "Changed source", translate.English
	v.startTranslate()
	tt := ui.NewTester(v.view, 400, 500)
	if !tt.HasText("旧译文") || !tt.HasText("上次译文 · 中文 · 原文已修改") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"取消翻译", "复制译文", "查看对应原文"} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.H <= 0 || r.X+r.W > 400 || r.Y+r.H > 500 {
			t.Fatalf("%s hidden: %+v", label, r)
		}
	}
	saveShortcutPreview(t, tt, "ocr-translate-pending-narrow")
	if err := tt.Click("复制译文"); err != nil {
		t.Fatal(err)
	}
	if *copied != "旧译文" {
		t.Fatal("pending request lost copyable result")
	}
	if err := tt.Click("取消翻译"); err != nil {
		t.Fatal(err)
	}
	work()
	if request.Err() != context.Canceled || v.tr.running || v.tr.text != "旧译文" || v.tr.resultSource != "Hello" {
		t.Fatal("cancellation took a late result or lost its source")
	}
	v.translator = func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{}, translate.ErrNotConfigured
	}
	v.run = func(fn func()) { fn() }
	v.startTranslate()
	tt.Frame()
	if !tt.HasText("旧译文") {
		t.Fatal("failed retry erased prior result")
	}
	saveShortcutPreview(t, tt, "ocr-translate-recovery-narrow")
	tt.SetSize(400, 400)
	for _, label := range []string{"旧译文", "复制译文", "重新翻译"} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.H <= 0 || r.X+r.W > 400 || r.Y+r.H > 400 {
			t.Fatalf("minimum window hides %s: %+v", label, r)
		}
	}
	saveShortcutPreview(t, tt, "ocr-translate-recovery-minimum")
	opened := false
	v.openSettings = func() { opened = true }
	if err := tt.Click("设置翻译服务"); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("failure offered no setup action")
	}
}

func TestSavedTranslationConfigReachesExistingTextWindows(t *testing.T) {
	previous := translateOffline
	defer func() { translateOffline = previous }()
	var directions []string
	translateOffline = func(_ context.Context, direction string, lines []string) ([]string, error) {
		directions = append(directions, direction)
		return []string{"新服务的译文"}, nil
	}
	v, _ := translatingWindow("", nil)
	v.setText("Hello", false)
	v.tab = 2
	s := &application{cfg: config.Default(), ocrWindows: []*ocrWindow{v}}
	v.setTranslationConfig(s.cfg)
	tt := ui.NewTester(v.view, 760, 620)
	if !tt.HasText("还没有设置翻译服务") {
		t.Fatal("missing initial setup prompt")
	}
	next := s.cfg
	next.TranslateProvider, next.TranslateTarget = string(translate.Offline), translate.Chinese
	s.setConfig(next)
	tt.Frame()
	if tt.HasText("还没有设置翻译服务") || !v.offline || v.target != translate.Chinese {
		t.Fatalf("existing window kept old settings: provider=%q target=%q", v.provider, v.target)
	}
	if len(directions) != 0 {
		t.Fatal("saving settings sent text automatically")
	}
	if err := tt.Click("重新翻译"); err != nil {
		t.Fatal(err)
	}
	if len(directions) != 1 || directions[0] != "en-zh" || !tt.HasText("新服务的译文") {
		t.Fatalf("directions=%v texts=%v", directions, tt.Texts())
	}
}

func TestTranslationConfigChangeDiscardsOldServiceResults(t *testing.T) {
	v, _ := translatingWindow("", nil)
	t.Cleanup(v.release)
	v.setText("Hello", false)
	cfg := config.Default()
	cfg.TranslateProvider = string(translate.DeepL)
	v.setTranslationConfig(cfg)
	s := &application{ctx: context.Background()}
	v.run = s.worker
	started := make(chan context.Context, 1)
	v.translator = func(ctx context.Context, _, _ string) (translate.Result, error) {
		started <- ctx
		<-ctx.Done()
		return translate.Result{Text: "old service"}, nil
	}
	deliveries := make(chan func(), 2)
	v.later = func(fn func()) { deliveries <- fn }
	v.startTranslate()
	var oldContext context.Context
	select {
	case oldContext = <-started:
	case <-time.After(time.Second):
		t.Fatal("old translation did not start")
	}
	v.autoTranslate = true
	cfg.TranslateProvider = string(translate.Offline)
	v.setTranslationConfig(cfg)
	if oldContext.Err() == nil || v.tr.running || v.tr.text != "" || v.autoTranslate {
		t.Fatal("changing services kept a pending translation")
	}
	var oldResult func()
	select {
	case oldResult = <-deliveries:
	case <-time.After(time.Second):
		t.Fatal("changing services did not cancel the old request")
	}
	v.translator = func(context.Context, string, string) (translate.Result, error) {
		return translate.Result{Text: "new service"}, nil
	}
	v.startTranslate()
	// Complete the new request before an old result queued on the UI thread.
	select {
	case newResult := <-deliveries:
		newResult()
	case <-time.After(time.Second):
		t.Fatal("new translation did not finish")
	}
	oldResult()
	s.workers.Wait()
	if v.tr.text != "new service" || v.tr.running {
		t.Fatalf("old service overwrote the current translation: %+v", v.tr)
	}
}

func TestUnrelatedSettingsPreserveTranslationAndWindowDirection(t *testing.T) {
	v := newOCRWindow()
	cfg := config.Default()
	cfg.TranslateProvider = string(translate.Offline)
	v.setTranslationConfig(cfg)
	v.target = translate.English // A direction selected in this window.
	v.tr = ocrTranslation{source: "Hello", text: "已有译文"}
	generation := v.trGeneration
	cfg.HideAfterCopy = !cfg.HideAfterCopy
	v.setTranslationConfig(cfg)
	if v.target != translate.English || v.tr.text != "已有译文" || v.trGeneration != generation {
		t.Fatal("unrelated preferences reset translation state")
	}
	cfg.TranslateTarget = translate.Chinese
	v.setTranslationConfig(cfg)
	if v.target != translate.Chinese || v.tr.text != "" {
		t.Fatal("a changed default direction did not invalidate the old translation")
	}
}

func TestApplicationCancellationStopsTextWindowTranslation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &application{ctx: ctx}
	started := make(chan context.Context, 1)
	deliveries := make(chan func(), 1)
	v, _ := translatingWindow("test service", func(ctx context.Context, _, _ string) (translate.Result, error) {
		started <- ctx
		<-ctx.Done()
		return translate.Result{}, ctx.Err()
	})
	v.ctx, v.run = ctx, s.worker
	v.later = func(fn func()) { deliveries <- fn }
	v.setText("Hello", false)
	v.startTranslate()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("translation did not start")
	}
	cancel()
	select {
	case deliver := <-deliveries:
		deliver()
	case <-time.After(time.Second):
		t.Fatal("translation survived application cancellation")
	}
	s.workers.Wait()
	if v.tr.text != "" || v.tr.err != "" {
		t.Fatal("shutdown delivered a translation result")
	}
	v.release()
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

func TestConfiguredTranslationUsesOfflineWithoutCredentials(t *testing.T) {
	load, offline := loadCredentials, translateOffline
	defer func() { loadCredentials, translateOffline = load, offline }()
	loadCredentials = func(translate.Provider) (credentials, error) {
		t.Error("offline translation opened the credential store")
		return credentials{}, errors.New("no credential store")
	}
	translateOffline = func(_ context.Context, direction string, lines []string) ([]string, error) {
		if direction != "en-zh" || len(lines) != 1 || lines[0] != "Screenshots, pinned and translated." {
			t.Errorf("direction=%q lines=%v", direction, lines)
		}
		return []string{"截图、钉图与翻译。"}, nil
	}
	cfg := config.Default()
	cfg.TranslateProvider = string(translate.Offline)
	result, err := translateConfigured(context.Background(), cfg, "Screenshots, pinned and translated.", translate.Chinese, credentials{})
	if err != nil || result.Text != "截图、钉图与翻译。" {
		t.Fatalf("offline sample: %+v %v", result, err)
	}
}

func TestConfiguredTranslationUsesEnteredOrStoredKeys(t *testing.T) {
	load := loadCredentials
	defer func() { loadCredentials = load }()
	loadCredentials = func(p translate.Provider) (credentials, error) {
		if p != translate.OpenAI {
			t.Errorf("unexpected provider %s", p)
		}
		return credentials{Key: "stored-key"}, nil
	}
	type request struct{ auth, model string }
	requests := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- request{auth: r.Header.Get("Authorization"), model: body.Model}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.TranslateProvider, cfg.TranslateBaseURL, cfg.TranslateModel = string(translate.OpenAI), srv.URL, "configured-model"
	for _, tc := range []struct{ entered, want string }{{"typed-key", "typed-key"}, {"  ", "stored-key"}} {
		result, err := translateConfigured(context.Background(), cfg, "Hello", translate.Chinese, credentials{Key: tc.entered})
		if err != nil || result.Text != "你好" {
			t.Fatalf("sample failed: %+v %v", result, err)
		}
		got := <-requests
		if got.auth != "Bearer "+tc.want || got.model != cfg.TranslateModel {
			t.Fatalf("request=%+v", got)
		}
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
