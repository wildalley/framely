package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/translate"
)

// secrets keeps translation keys under the product's name, the same for a
// development run and the installed app.
var secrets = mygo.Secrets.Service(brand.Name)

// credentials are one provider's ID and key, kept together as one secret.
type credentials struct {
	ID  string `json:"id,omitempty"`
	Key string `json:"key"`
}

func secretName(p translate.Provider) string { return "translate." + string(p) }

// loadCredentials reads a provider's keys. It may block while the system
// unlocks its credential store: call it off the main thread.
var loadCredentials = func(p translate.Provider) (credentials, error) {
	data, err := secrets.Get(secretName(p))
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return credentials{}, nil
	}
	if err != nil {
		return credentials{}, err
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return credentials{}, errors.New("保存的翻译密钥已损坏，请重新填写")
	}
	return c, nil
}

// saveCredentials stores a provider's keys, or deletes them when both are
// empty. It may block: call it off the main thread.
var saveCredentials = func(p translate.Provider, c credentials) error {
	c.ID, c.Key = strings.TrimSpace(c.ID), strings.TrimSpace(c.Key)
	if c.ID == "" && c.Key == "" {
		if err := secrets.Delete(secretName(p)); err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
			return err
		}
		return nil
	}
	data, _ := json.Marshal(c)
	if err := secrets.Set(secretName(p), data); err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			return errors.New("系统没有可用的密钥存储（如 GNOME Keyring、KWallet），无法保存翻译密钥")
		}
		return err
	}
	return nil
}

// translateWith translates text with the service in cfg. Call it off the
// main thread: it reads the credential store and waits on the network.
func translateWith(ctx context.Context, cfg config.Config, text, target string) (translate.Result, error) {
	p := translate.Provider(cfg.TranslateProvider)
	if p == translate.None {
		return translate.Result{}, translate.ErrNotConfigured
	}
	c, err := loadCredentials(p)
	if err != nil {
		return translate.Result{}, err
	}
	return translate.Translate(ctx, translate.Options{
		Provider: p, ID: c.ID, Key: c.Key, BaseURL: cfg.TranslateBaseURL, Model: cfg.TranslateModel, Target: target,
	}, text)
}

// storeCredentials merges what was typed into the stored keys: a blank
// field keeps what is stored. Call it off the main thread.
func storeCredentials(p translate.Provider, id, key string) error {
	c, err := loadCredentials(p)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) != "" {
		c.ID = id
	}
	if strings.TrimSpace(key) != "" {
		c.Key = key
	}
	return saveCredentials(p, c)
}

var providerLabels = func() []string {
	labels := []string{translate.None.Label()}
	for _, p := range translate.Providers {
		labels = append(labels, p.Label())
	}
	return labels
}()

func providerOf(label string) translate.Provider {
	for _, p := range translate.Providers {
		if p.Label() == label {
			return p
		}
	}
	return translate.None
}

// translateSettings chooses the service and takes its keys. Keys go to the
// system credential store on save and are never shown again.
func (v *settingsWindow) translateSettings(c *ui.Context) {
	t := c.Theme()
	provider := translate.Provider(v.draft.TranslateProvider)
	if v.trStored == nil {
		v.trStored = map[string]bool{}
		v.app.worker(func() {
			stored := map[string]bool{}
			for _, p := range translate.Providers {
				if creds, err := loadCredentials(p); err == nil && creds.Key != "" {
					stored[string(p)] = true
				}
			}
			v.later(func() { v.trStored = stored })
		})
	}
	ui.Text(c, "翻译").FontWeight(600)
	ui.Text(c, "取字后可翻译识别出的文字。只在你打开「翻译」或点「重新翻译」时发送文字，从不发送图片；密钥保存在系统密钥库，不写入配置文件。").FontSize(12).TextColor(t.TextMuted).MaxLines(4)
	field := func(label string, children func()) {
		ui.Column(c).Gap(4).FillWidth().Children(func() {
			ui.Text(c, label).FontSize(12)
			children()
		})
	}
	field("翻译服务", func() {
		choice := provider.Label()
		if ui.Select(c.Key("translate-provider"), &choice, providerLabels).Label("选择翻译服务").Width(240).Disabled(v.saving).Changed() {
			v.draft.TranslateProvider = string(providerOf(choice))
			v.trID, v.trKey, v.trResult = "", "", ""
		}
	})
	if provider == translate.None {
		return
	}
	if provider.NeedsID() {
		label := "App ID"
		if provider == translate.Tencent {
			label = "SecretId"
		}
		field(label, func() {
			placeholder := "填写 " + label
			if v.trStored[string(provider)] {
				placeholder = "已保存，留空不修改"
			}
			ui.TextInput(c.Key("translate-id"), &v.trID).Label(label).Placeholder(placeholder).FillWidth().Disabled(v.saving)
		})
	}
	keyLabel := map[translate.Provider]string{translate.DeepL: "API Key", translate.Baidu: "密钥", translate.Tencent: "SecretKey", translate.OpenAI: "API Key"}[provider]
	field(keyLabel, func() {
		placeholder := "粘贴" + keyLabel
		if v.trStored[string(provider)] {
			placeholder = "已保存，留空不修改"
		}
		ui.TextInput(c.Key("translate-key"), &v.trKey).Password().Label(keyLabel).Placeholder(placeholder).FillWidth().Disabled(v.saving)
	})
	if provider == translate.OpenAI {
		field("接口地址", func() {
			ui.TextInput(c.Key("translate-base"), &v.draft.TranslateBaseURL).Label("接口地址").Placeholder("https://api.openai.com/v1").FillWidth().Disabled(v.saving)
		})
		field("模型", func() {
			ui.TextInput(c.Key("translate-model"), &v.draft.TranslateModel).Label("模型").Placeholder("例如 gpt-4o-mini、deepseek-chat、qwen-plus").FillWidth().Disabled(v.saving)
		})
	}
	field("默认翻译方向", func() {
		choice := targetLabels[0]
		for i, value := range targetValues {
			if value == v.draft.TranslateTarget {
				choice = targetLabels[i]
			}
		}
		if ui.Select(c.Key("translate-default-target"), &choice, targetLabels).Label("选择默认翻译方向").Width(160).Disabled(v.saving).Changed() {
			for i, label := range targetLabels {
				if label == choice {
					v.draft.TranslateTarget = targetValues[i]
				}
			}
		}
		ui.Text(c, "自动：中文译成英文，其他语言译成中文。").FontSize(12).TextColor(t.TextMuted)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
		label := "测试翻译"
		if v.trTesting {
			label = "测试中…"
		}
		if ui.Button(c.Key("translate-test"), label).Disabled(v.trTesting || v.saving).Clicked() {
			v.testTranslation()
		}
		ui.Text(c, v.trResult).FontSize(12).TextColor(t.TextMuted).MaxLines(3).Grow(1).MinWidth(0).Selectable()
	})
}

// testTranslation translates a sample with what is typed, or with what is
// stored where a field is left blank, without saving anything.
func (v *settingsWindow) testTranslation() {
	provider, id, key, cfg := translate.Provider(v.draft.TranslateProvider), v.trID, v.trKey, v.draft
	v.trTesting, v.trResult = true, ""
	v.app.worker(func() {
		c, err := loadCredentials(provider)
		if strings.TrimSpace(id) != "" {
			c.ID = id
		}
		if strings.TrimSpace(key) != "" {
			c.Key = key
		}
		var result translate.Result
		if err == nil {
			ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
			result, err = translate.Translate(ctx, translate.Options{
				Provider: provider, ID: c.ID, Key: c.Key, BaseURL: cfg.TranslateBaseURL, Model: cfg.TranslateModel, Target: translate.Chinese,
			}, "Screenshots, pinned and translated.")
			cancel()
		}
		v.later(func() {
			v.trTesting = false
			if err != nil {
				v.trResult = "失败：" + err.Error()
				return
			}
			v.trResult = "可用：" + result.Text
		})
	})
}
