package recording

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wildalley/framely/internal/components"
)

var durationLine = regexp.MustCompile(`(?m)^  Duration: (\d+):([0-5]\d):([0-5]\d(?:\.\d+)?),`)

const durationTimeout = 5 * time.Second

func durationFromLog(log string) (float64, bool) {
	parts := durationLine.FindStringSubmatch(log)
	if len(parts) != 4 {
		return 0, false
	}
	var seconds float64
	for i, factor := range []float64{3600, 60, 1} {
		value, err := strconv.ParseFloat(parts[i+1], 64)
		if err != nil {
			return 0, false
		}
		seconds += value * factor
	}
	return seconds, seconds > 0 && !math.IsInf(seconds, 0)
}

// Asking FFmpeg to inspect an input without an output reads format metadata
// and its seek index, never decodes/transcodes the whole recording. Its normal
// "no output file" exit status is expected; only a positive duration counts.
func probeDuration(parent context.Context, path string) (float64, error) {
	ffmpeg, err := components.Find("framely-ffmpeg")
	if err != nil {
		return 0, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(parent, ffmpeg, "-hide_banner", "-loglevel", "info", "-nostdin", "-protocol_whitelist", "file,pipe", "-i", path)
	return runDurationProbe(parent, cmd)
}

func runDurationProbe(ctx context.Context, cmd *exec.Cmd) (float64, error) {
	hideWindow(cmd)
	var log limitedLog
	cmd.Stderr, cmd.Stdout = &log, io.Discard
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动视频时长探测失败：%w", err)
	}
	err := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// FFmpeg returns 1 for a successful input-only inspection. Any other
	// process failure must remain visible even if metadata was printed first.
	var exit *exec.ExitError
	if err == nil || errors.As(err, &exit) && exit.ExitCode() == 1 {
		if duration, ok := durationFromLog(log.String()); ok {
			return duration, nil
		}
	}
	if err != nil {
		return 0, fmt.Errorf("读取视频时长失败：%w\n%s", err, strings.TrimSpace(log.String()))
	}
	if detail := strings.TrimSpace(log.String()); detail != "" {
		return 0, fmt.Errorf("无法读取视频时长：%s", detail)
	}
	return 0, errors.New("无法读取视频时长")
}
