// Package translate sends recognized text to the translation service the
// user configured with their own key. Only text is sent, never images, and
// only when the user asks for a translation.
package translate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Provider names a translation service.
type Provider string

const (
	None    Provider = ""
	DeepL   Provider = "deepl"
	Baidu   Provider = "baidu"
	Tencent Provider = "tencent"
	OpenAI  Provider = "openai" // any OpenAI-compatible chat completions API
)

// Providers are the services offered, in the order settings lists them.
var Providers = []Provider{DeepL, Baidu, Tencent, OpenAI}

// Label is the provider's name as settings shows it.
func (p Provider) Label() string {
	switch p {
	case DeepL:
		return "DeepL"
	case Baidu:
		return "百度翻译"
	case Tencent:
		return "腾讯翻译君"
	case OpenAI:
		return "大模型（OpenAI 兼容）"
	}
	return "未启用"
}

// NeedsID reports whether the provider takes an ID beside its key: Baidu's
// App ID or Tencent's SecretId.
func (p Provider) NeedsID() bool { return p == Baidu || p == Tencent }

// Target languages.
const (
	Auto    = ""   // Chinese text into English, anything else into Chinese
	Chinese = "zh" // Simplified Chinese
	English = "en"
)

// MaxRunes is the most text one translation sends.
const MaxRunes = 20000

// Options configure one translation.
type Options struct {
	Provider Provider
	ID, Key  string // credentials; ID only for Baidu and Tencent
	BaseURL  string // OpenAI-compatible API root, as https://api.openai.com/v1
	Model    string // OpenAI-compatible model
	Target   string // Auto, Chinese or English
	// Endpoint replaces the provider's URL, for tests.
	Endpoint string
	Client   *http.Client
	now      func() time.Time
}

// Result is the translated text and the language it went into.
type Result struct {
	Text   string
	Target string
}

// ErrNotConfigured is returned when no provider or key is set.
var ErrNotConfigured = errors.New("未设置翻译服务：请在「设置与快捷键 › 翻译」中选择服务并填写密钥")

// Translate translates text with o. Text longer than one request allows is
// sent in pieces split at line breaks, one after another.
func Translate(ctx context.Context, o Options, text string) (Result, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return Result{}, errors.New("没有可翻译的文字")
	}
	if n := utf8.RuneCountInString(text); n > MaxRunes {
		return Result{}, fmt.Errorf("文字过长（%d 字），一次最多翻译 %d 字", n, MaxRunes)
	}
	if o.Provider == None || strings.TrimSpace(o.Key) == "" || o.Provider.NeedsID() && strings.TrimSpace(o.ID) == "" {
		return Result{}, ErrNotConfigured
	}
	if o.Provider == OpenAI && strings.TrimSpace(o.Model) == "" {
		return Result{}, errors.New("请填写大模型名称")
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if o.now == nil {
		o.now = time.Now
	}
	target := o.Target
	if target == Auto {
		target = Detect(text)
	}
	var translate func(context.Context, Options, string, string) (string, error)
	limit := 0
	switch o.Provider {
	case DeepL:
		translate, limit = deepL, MaxRunes
	case Baidu:
		translate, limit = baidu, 1500
	case Tencent:
		translate, limit = tencent, 1500
	case OpenAI:
		translate, limit = openAI, 6000
	default:
		return Result{}, fmt.Errorf("不支持的翻译服务：%s", o.Provider)
	}
	var out []string
	for _, piece := range split(text, limit) {
		got, err := translate(ctx, o, piece, target)
		if err != nil {
			return Result{}, err
		}
		out = append(out, strings.TrimSpace(got))
	}
	return Result{Text: strings.Join(out, "\n"), Target: target}, nil
}

// Detect is the language to translate text into: English for text that is
// mostly Chinese, Chinese for anything else.
func Detect(text string) string {
	han, letters := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.IsLetter(r):
			letters++
		}
	}
	// A Chinese character carries about as much as a short word.
	if han > 0 && han*3 >= letters {
		return English
	}
	return Chinese
}

// split cuts text into pieces of at most limit runes at line breaks, and
// within a line too long for one piece.
func split(text string, limit int) []string {
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var pieces []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			pieces = append(pieces, string(current))
			current = current[:0]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for len(runes) > limit {
			flush()
			pieces = append(pieces, string(runes[:limit]))
			runes = runes[limit:]
		}
		if len(current)+len(runes)+1 > limit {
			flush()
		}
		if len(current) > 0 {
			current = append(current, '\n')
		}
		current = append(current, runes...)
	}
	flush()
	return pieces
}

// statusError explains an HTTP failure in the user's terms.
func statusError(service string, res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	detail := strings.TrimSpace(string(body))
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s：密钥无效或没有权限（HTTP %d）", service, res.StatusCode)
	case http.StatusTooManyRequests, 456:
		return fmt.Errorf("%s：请求过于频繁或额度已用完（HTTP %d）", service, res.StatusCode)
	}
	if detail != "" {
		return fmt.Errorf("%s：HTTP %d：%s", service, res.StatusCode, detail)
	}
	return fmt.Errorf("%s：HTTP %d", service, res.StatusCode)
}

// readLimit caps a response; translations of MaxRunes fit well within it.
const readLimit = 4 << 20
