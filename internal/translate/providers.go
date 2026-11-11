package translate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func post(ctx context.Context, o Options, service, endpoint string, body io.Reader, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("%s：%w", service, err)
	}
	req.Header = header
	res, err := o.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s：无法连接：%w", service, err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return statusError(service, res)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, readLimit)).Decode(out); err != nil {
		return fmt.Errorf("%s：无法读取结果：%w", service, err)
	}
	return nil
}

// deepL uses the free API for keys ending in ":fx", as DeepL issues them.
func deepL(ctx context.Context, o Options, text, target string) (string, error) {
	endpoint := "https://api.deepl.com/v2/translate"
	if strings.HasSuffix(strings.TrimSpace(o.Key), ":fx") {
		endpoint = "https://api-free.deepl.com/v2/translate"
	}
	if o.Endpoint != "" {
		endpoint = o.Endpoint
	}
	lang := "ZH-HANS"
	if target == English {
		lang = "EN-US"
	}
	body, _ := json.Marshal(map[string]any{"text": []string{text}, "target_lang": lang})
	header := http.Header{"Content-Type": {"application/json"}, "Authorization": {"DeepL-Auth-Key " + strings.TrimSpace(o.Key)}}
	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := post(ctx, o, "DeepL", endpoint, bytes.NewReader(body), header, &out); err != nil {
		return "", err
	}
	if len(out.Translations) == 0 {
		return "", errors.New("DeepL：没有返回译文")
	}
	return out.Translations[0].Text, nil
}

// baidu signs each request with md5(appid+q+salt+key).
func baidu(ctx context.Context, o Options, text, target string) (string, error) {
	endpoint := "https://fanyi-api.baidu.com/api/trans/vip/translate"
	if o.Endpoint != "" {
		endpoint = o.Endpoint
	}
	id, key := strings.TrimSpace(o.ID), strings.TrimSpace(o.Key)
	salt := strconv.FormatInt(o.now().UnixNano(), 10)
	sum := md5.Sum([]byte(id + text + salt + key))
	form := url.Values{"q": {text}, "from": {"auto"}, "to": {target}, "appid": {id}, "salt": {salt}, "sign": {hex.EncodeToString(sum[:])}}
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	var out struct {
		Code    string `json:"error_code"`
		Message string `json:"error_msg"`
		Result  []struct {
			Dst string `json:"dst"`
		} `json:"trans_result"`
	}
	if err := post(ctx, o, "百度翻译", endpoint, strings.NewReader(form.Encode()), header, &out); err != nil {
		return "", err
	}
	if out.Code != "" && out.Code != "52000" {
		switch out.Code {
		case "52003", "54001":
			return "", fmt.Errorf("百度翻译：App ID 或密钥无效（%s）", out.Code)
		case "54003", "54004", "54005":
			return "", fmt.Errorf("百度翻译：请求过于频繁或余额不足（%s）", out.Code)
		}
		return "", fmt.Errorf("百度翻译：%s %s", out.Code, out.Message)
	}
	lines := make([]string, 0, len(out.Result))
	for _, r := range out.Result {
		lines = append(lines, r.Dst)
	}
	return strings.Join(lines, "\n"), nil
}

const (
	tencentHost    = "tmt.tencentcloudapi.com"
	tencentService = "tmt"
	tencentAction  = "TextTranslate"
	tencentVersion = "2018-03-21"
	tencentRegion  = "ap-guangzhou"
)

// tencent signs the request with TC3-HMAC-SHA256, as Tencent Cloud's API 3.0
// requires.
func tencent(ctx context.Context, o Options, text, target string) (string, error) {
	endpoint := "https://" + tencentHost
	if o.Endpoint != "" {
		endpoint = o.Endpoint
	}
	payload, _ := json.Marshal(map[string]any{"SourceText": text, "Source": "auto", "Target": target, "ProjectId": 0})
	now := o.now().UTC()
	header := http.Header{
		"Content-Type":   {"application/json; charset=utf-8"},
		"Host":           {tencentHost},
		"X-TC-Action":    {tencentAction},
		"X-TC-Timestamp": {strconv.FormatInt(now.Unix(), 10)},
		"X-TC-Version":   {tencentVersion},
		"X-TC-Region":    {tencentRegion},
		"Authorization":  {tencentAuthorization(strings.TrimSpace(o.ID), strings.TrimSpace(o.Key), payload, now.Unix())},
	}
	var out struct {
		Response struct {
			TargetText string `json:"TargetText"`
			Error      *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := post(ctx, o, "腾讯翻译君", endpoint, bytes.NewReader(payload), header, &out); err != nil {
		return "", err
	}
	if e := out.Response.Error; e != nil {
		if strings.HasPrefix(e.Code, "AuthFailure") {
			return "", fmt.Errorf("腾讯翻译君：SecretId 或 SecretKey 无效（%s）", e.Code)
		}
		return "", fmt.Errorf("腾讯翻译君：%s %s", e.Code, e.Message)
	}
	return out.Response.TargetText, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func tencentAuthorization(id, key string, payload []byte, timestamp int64) string {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	canonical := "POST\n/\n\n" +
		"content-type:application/json; charset=utf-8\nhost:" + tencentHost + "\nx-tc-action:" + strings.ToLower(tencentAction) + "\n\n" +
		"content-type;host;x-tc-action\n" + sha256Hex(payload)
	scope := date + "/" + tencentService + "/tc3_request"
	toSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(timestamp, 10) + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	signing := hmacSHA256(hmacSHA256(hmacSHA256([]byte("TC3"+key), date), tencentService), "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(signing, toSign))
	return "TC3-HMAC-SHA256 Credential=" + id + "/" + scope + ", SignedHeaders=content-type;host;x-tc-action, Signature=" + signature
}

// openAI asks a chat model for the translation alone.
func openAI(ctx context.Context, o Options, text, target string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	endpoint := base + "/chat/completions"
	if o.Endpoint != "" {
		endpoint = o.Endpoint
	}
	language := "Simplified Chinese"
	if target == English {
		language = "English"
	}
	body, _ := json.Marshal(map[string]any{
		"model":       strings.TrimSpace(o.Model),
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": "You translate text recognized from a screenshot into " + language + ". Keep the line breaks. Reply with the translation only, without notes or quotes."},
			{"role": "user", "content": text},
		},
	})
	header := http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer " + strings.TrimSpace(o.Key)}}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := post(ctx, o, "大模型", endpoint, bytes.NewReader(body), header, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("大模型：没有返回译文")
	}
	return out.Choices[0].Message.Content, nil
}
