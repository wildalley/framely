package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wildalley/framely/internal/components"
)

// Translate runs a downloaded offline model in the OCR worker's translate
// mode: one process per request, which loads the model, translates the
// lines and exits, so nothing stays resident. It returns a line for each.
func Translate(ctx context.Context, modelDir, direction string, lines []string) ([]string, error) {
	path, err := components.Find("framely-ocr")
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Translate int `json:"translate"`
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json")); err != nil || json.Unmarshal(data, &manifest) != nil || manifest.Translate < 1 {
		return nil, errors.New("离线翻译需要新版文字识别组件，请重新安装 Framely")
	}
	request, err := json.Marshal(map[string][]string{"lines": lines})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--translate", modelDir, direction)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(request)
	stderr := &limitedBuffer{limit: 8 << 10}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动离线翻译失败：%w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(out, 8<<20))
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("离线翻译超时")
		}
		return nil, ctx.Err()
	}
	if readErr != nil {
		return nil, readErr
	}
	var result struct {
		Lines []string `json:"lines"`
		Error string   `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &result); err != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("离线翻译失败：%v %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("离线翻译结果无效：%w", err)
	}
	if result.Error != "" {
		return nil, errors.New("离线翻译失败：" + result.Error)
	}
	if len(result.Lines) != len(lines) {
		return nil, errors.New("离线翻译结果行数不符")
	}
	return result.Lines, nil
}
