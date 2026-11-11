package translate

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func server(t *testing.T, handle func(*http.Request, []byte) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		status, reply := handle(r, body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

var fixed = func() time.Time { return time.Date(2026, 11, 11, 23, 30, 0, 0, time.FixedZone("CST", 8*3600)) }

func TestDeepLSendsTheKeyAndTarget(t *testing.T) {
	srv := server(t, func(r *http.Request, body []byte) (int, string) {
		var in struct {
			Text   []string `json:"text"`
			Target string   `json:"target_lang"`
		}
		_ = json.Unmarshal(body, &in)
		if r.Header.Get("Authorization") != "DeepL-Auth-Key k:fx" || in.Target != "ZH-HANS" || in.Text[0] != "Hello\nWorld" {
			return 400, "bad request"
		}
		return 200, `{"translations":[{"detected_source_language":"EN","text":"你好\n世界"}]}`
	})
	got, err := Translate(context.Background(), Options{Provider: DeepL, Key: "k:fx", Endpoint: srv.URL}, " Hello\r\nWorld ")
	if err != nil || got.Text != "你好\n世界" || got.Target != Chinese {
		t.Fatal(got, err)
	}
}

func TestBaiduSignsWithMD5AndJoinsLines(t *testing.T) {
	srv := server(t, func(r *http.Request, body []byte) (int, string) {
		form, _ := url.ParseQuery(string(body))
		sum := md5.Sum([]byte("app" + form.Get("q") + form.Get("salt") + "secret"))
		if form.Get("sign") != hex.EncodeToString(sum[:]) || form.Get("to") != "en" || form.Get("from") != "auto" {
			return 200, `{"error_code":"54001","error_msg":"Invalid Sign"}`
		}
		return 200, `{"from":"zh","to":"en","trans_result":[{"src":"你好","dst":"Hello"},{"src":"世界","dst":"World"}]}`
	})
	o := Options{Provider: Baidu, ID: "app", Key: "secret", Endpoint: srv.URL, now: fixed}
	got, err := Translate(context.Background(), o, "你好\n世界")
	if err != nil || got.Text != "Hello\nWorld" || got.Target != English {
		t.Fatal(got, err)
	}
	o.Key = "wrong"
	if _, err := Translate(context.Background(), o, "你好"); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("a bad signature: %v", err)
	}
}

func TestTencentSignsTheRequestWithTC3(t *testing.T) {
	var auth string
	srv := server(t, func(r *http.Request, body []byte) (int, string) {
		auth = r.Header.Get("Authorization")
		var in struct{ SourceText, Target string }
		_ = json.Unmarshal(body, &in)
		if r.Header.Get("X-TC-Action") != "TextTranslate" || r.Header.Get("X-TC-Timestamp") != "1794411000" || in.Target != "zh" {
			return 400, ""
		}
		return 200, `{"Response":{"TargetText":"你好","Source":"en","Target":"zh","RequestId":"x"}}`
	})
	got, err := Translate(context.Background(), Options{Provider: Tencent, ID: "AKID", Key: "sk", Endpoint: srv.URL, now: fixed}, "Hello")
	if err != nil || got.Text != "你好" {
		t.Fatal(got, err)
	}
	// The date in the scope is UTC: 23:30 in UTC+8 is still the 11th there.
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKID/2026-11-11/tmt/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=") {
		t.Fatal(auth)
	}
	if a, b := tencentAuthorization("AKID", "sk", []byte("a"), 1), tencentAuthorization("AKID", "sk", []byte("b"), 1); a == b {
		t.Fatal("the signature does not cover the payload")
	}
}

func TestTencentReportsAuthFailures(t *testing.T) {
	srv := server(t, func(*http.Request, []byte) (int, string) {
		return 200, `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"x"},"RequestId":"x"}}`
	})
	_, err := Translate(context.Background(), Options{Provider: Tencent, ID: "AKID", Key: "sk", Endpoint: srv.URL}, "Hello")
	if err == nil || !strings.Contains(err.Error(), "SecretKey 无效") {
		t.Fatal(err)
	}
}

func TestOpenAICompatibleAsksForTheTranslationOnly(t *testing.T) {
	srv := server(t, func(r *http.Request, body []byte) (int, string) {
		var in struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		_ = json.Unmarshal(body, &in)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-1" || in.Model != "m" ||
			!strings.Contains(in.Messages[0].Content, "English") || in.Messages[1].Content != "你好" {
			return 400, ""
		}
		return 200, `{"choices":[{"message":{"role":"assistant","content":"Hello"}}]}`
	})
	got, err := Translate(context.Background(), Options{Provider: OpenAI, Key: "sk-1", BaseURL: srv.URL + "/v1/", Model: "m"}, "你好")
	if err != nil || got.Text != "Hello" {
		t.Fatal(got, err)
	}
}

func TestHTTPFailuresAreExplained(t *testing.T) {
	for status, want := range map[int]string{401: "密钥无效", 403: "密钥无效", 429: "额度", 456: "额度", 500: "HTTP 500"} {
		srv := server(t, func(*http.Request, []byte) (int, string) { return status, "oops" })
		_, err := Translate(context.Background(), Options{Provider: DeepL, Key: "k", Endpoint: srv.URL}, "Hello")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%d: %v", status, err)
		}
	}
}

func TestMissingSettingsAndLimits(t *testing.T) {
	ctx := context.Background()
	for _, o := range []Options{{}, {Provider: DeepL}, {Provider: Baidu, Key: "k"}, {Provider: Tencent, ID: "id"}} {
		if _, err := Translate(ctx, o, "Hello"); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("%+v: %v", o, err)
		}
	}
	if _, err := Translate(ctx, Options{Provider: OpenAI, Key: "k"}, "Hello"); err == nil {
		t.Fatal("an OpenAI-compatible service without a model was accepted")
	}
	if _, err := Translate(ctx, Options{Provider: DeepL, Key: "k"}, strings.Repeat("字", MaxRunes+1)); err == nil {
		t.Fatal("over-long text was sent")
	}
	if _, err := Translate(ctx, Options{Provider: DeepL, Key: "k"}, " \n "); err == nil {
		t.Fatal("empty text was sent")
	}
}

func TestLongTextIsSentInPiecesAtLineBreaks(t *testing.T) {
	var sizes []int
	srv := server(t, func(r *http.Request, body []byte) (int, string) {
		form, _ := url.ParseQuery(string(body))
		sizes = append(sizes, utf8.RuneCountInString(form.Get("q")))
		return 200, `{"trans_result":[{"dst":"x"}]}`
	})
	line := strings.Repeat("字", 600)
	text := strings.Join([]string{line, line, line, strings.Repeat("长", 3500)}, "\n")
	got, err := Translate(context.Background(), Options{Provider: Baidu, ID: "a", Key: "k", Endpoint: srv.URL}, text)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sizes {
		if n > 1500 {
			t.Fatalf("a request of %d runes: %v", n, sizes)
		}
	}
	if len(sizes) != 5 || got.Text != strings.Repeat("x\n", 4)+"x" {
		t.Fatalf("pieces %v, text %q", sizes, got.Text)
	}
}

func TestDetectTranslatesChineseIntoEnglishAndTheRestIntoChinese(t *testing.T) {
	for text, want := range map[string]string{
		"截图之后，让文字自由组合":                                           English,
		"Hello, world":                                           Chinese,
		"ScreenPin 支持本地识别":                                       English,
		"Windows 10 / Linux 版本 0.2.1 release notes for everyone": Chinese,
		"こんにちは":                                                  Chinese,
	} {
		if got := Detect(text); got != want {
			t.Errorf("%q: %s, want %s", text, got, want)
		}
	}
}
