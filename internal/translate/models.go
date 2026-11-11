package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ModelFile is one file of an offline model, pinned by size and SHA-256.
type ModelFile struct {
	Name   string
	Size   int64
	SHA256 string
}

// Model is a downloadable offline translation model for one direction.
type Model struct {
	Direction string // "en-zh" or "zh-en"
	Label     string
	Files     []ModelFile
}

// Size is the model's download size in bytes.
func (m Model) Size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// ModelBaseURL is where the model files are published, as
// <direction>-<file name> assets of one GitHub Release.
var ModelBaseURL = "https://github.com/wildalley/framely/releases/download/translate-models-v1/"

// Models are OPUS-MT models (University of Helsinki) in the 8-bit ONNX
// export of Xenova, redistributed unmodified with their licenses in
// NOTICE.txt: Apache-2.0 for English to Chinese, CC BY 4.0 for Chinese to
// English.
var Models = []Model{
	{Direction: "en-zh", Label: "英 → 中", Files: []ModelFile{
		{"encoder_model_quantized.onnx", 52899742, "d3b7912bf6a9bd27e4c074c2df91d4ff3d5b4bc5f7f6c8d7cc9c805c98fbafee"},
		{"decoder_model_quantized.onnx", 59842102, "2c66a3981099b40edbb3a0d65e015d05964d42fecb9780f8422776eff5939112"},
		{"source.spm", 806435, "5775ddc9e3ff2fae91554da56468ad35ff56edaba870fea74447bc7234bfdaa8"},
		{"target.spm", 804600, "81dc94efa84e4025ef38d25d5d07429fe41e3eb29d44003f1db6fe98487b0052"},
		{"vocab.json", 1747795, "22c957348eed495ee925afc40a36da3e387c8a34a734c8486967c2dca271613e"},
		{"NOTICE.txt", 502, "dfa6e0d090afa73f69d478cdd4f5fb9d1aff5ae5b76539101512eae9b8c3d4df"},
	}},
	{Direction: "zh-en", Label: "中 → 英", Files: []ModelFile{
		{"encoder_model_quantized.onnx", 52899742, "84d5e171b626bc8b6b220d022ac58696e9528c25deeacca62b5cbf4364547a99"},
		{"decoder_model_quantized.onnx", 59842102, "debcc3054b9ff9aaed972bd6e251a459abe1e2f1402da1efa63163dc2f46ef73"},
		{"source.spm", 804677, "e27a3a1b539f4959ec72ea60e453f49156289f95d4e6000b29332efc45616203"},
		{"target.spm", 806530, "6a881f4717cd7265f53fea54fd3dc689c767c05338fac7a4590f3088cb2d7855"},
		{"vocab.json", 1747906, "08a119a1defd522fa047cb5e3bfe3e89633e96caa38ced0dc9cee7ef1021a011"},
		{"NOTICE.txt", 543, "b2c314c9684605ef392b7db40b8ab86463fa4c3b6a5d7dd92fb20368fd3e8403"},
	}},
}

// ModelFor is the model translating into target ("zh" or "en").
func ModelFor(target string) (Model, bool) {
	direction := "en-zh"
	if target == English {
		direction = "zh-en"
	}
	for _, m := range Models {
		if m.Direction == direction {
			return m, true
		}
	}
	return Model{}, false
}

// Installed reports whether every file of m is in root at its pinned size.
// Downloads were checked against their hashes before being put there.
func Installed(root string, m Model) bool {
	for _, f := range m.Files {
		info, err := os.Stat(filepath.Join(root, m.Direction, f.Name))
		if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
			return false
		}
	}
	return true
}

// Remove deletes m from root.
func Remove(root string, m Model) error {
	if m.Direction == "" || strings.ContainsAny(m.Direction, `/\.`) {
		return errors.New("模型名称无效")
	}
	return os.RemoveAll(filepath.Join(root, m.Direction))
}

// Download fetches m into root, only when the user asks. Each file is
// written beside its place as .part, hashed as it arrives, and moved into
// place only when its size and SHA-256 match, so a cancelled or corrupted
// download never looks installed. Files already in place are kept.
// progress gets the bytes done and the total.
func Download(ctx context.Context, client *http.Client, root string, m Model, progress func(done, total int64)) error {
	if client == nil {
		// A whole model takes minutes on a slow link; the context cancels it.
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	dir := filepath.Join(root, m.Direction)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	total, done := m.Size(), int64(0)
	for _, f := range m.Files {
		path := filepath.Join(dir, f.Name)
		if info, err := os.Stat(path); err == nil && info.Size() == f.Size {
			if sum, err := fileSHA256(path); err == nil && sum == f.SHA256 {
				done += f.Size
				if progress != nil {
					progress(done, total)
				}
				continue
			}
		}
		base := done
		err := fetch(ctx, client, ModelBaseURL+m.Direction+"-"+f.Name, path, f, func(n int64) {
			if progress != nil {
				progress(base+n, total)
			}
		})
		if err != nil {
			return err
		}
		done += f.Size
	}
	return nil
}

func fetch(ctx context.Context, client *http.Client, url, path string, f ModelFile, progress func(int64)) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("下载模型失败：%w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载模型失败：%s HTTP %d", f.Name, res.StatusCode)
	}
	part := path + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if out != nil {
			out.Close()
		}
		if err != nil {
			os.Remove(part)
		}
	}()
	hash := sha256.New()
	counted := &countingWriter{progress: progress}
	// One byte past the pinned size proves a longer file wrong.
	n, err := io.Copy(io.MultiWriter(out, hash, counted), io.LimitReader(res.Body, f.Size+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("下载模型失败：%w", err)
	}
	if n != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("模型文件 %s 校验失败，已删除，请重试", f.Name)
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		out = nil
		return err
	}
	out = nil
	return os.Rename(part, path)
}

type countingWriter struct {
	n        int64
	progress func(int64)
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	w.progress(w.n)
	return len(p), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
