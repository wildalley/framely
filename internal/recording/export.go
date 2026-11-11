// Package recording owns bounded, cancellable media exports and private staging files.
package recording

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wildalley/framely/internal/components"
)

type GIFOptions struct {
	Start, Duration float64
	FPS, Width      int
	// Progress, when set, receives the share of the export done, 0 to 1,
	// on the exporting goroutine.
	Progress func(done float64)
}

func (o GIFOptions) Validate() error {
	if math.IsNaN(o.Start) || math.IsInf(o.Start, 0) || math.IsNaN(o.Duration) || math.IsInf(o.Duration, 0) || o.Start < 0 || o.Start > 86400 || o.Duration <= 0 || o.Duration > 60 || (o.FPS != 10 && o.FPS != 15 && o.FPS != 20 && o.FPS != 30) || o.Width < 160 || o.Width > 1920 {
		return errors.New("GIF 范围需在 1 分钟内，帧率为 10/15/20/30，宽度为 160–1920")
	}
	return nil
}
func Stage(destination string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(destination), ".framely-record-*.mp4")
	if err != nil {
		return "", err
	}
	name := f.Name()
	err = f.Close()
	if err != nil {
		_ = os.Remove(name)
	}
	return name, err
}
func Publish(staged, destination string) error {
	f, err := os.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && info.Size() == 0 {
		err = errors.New("录屏没有生成有效文件")
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(staged, destination)
}
func SaveCopy(ctx context.Context, source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := Stage(destination)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	out, err := os.OpenFile(temp, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	buffer := make([]byte, 128<<10)
	for err == nil {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = in.Read(buffer)
		if n > 0 {
			_, writeErr := out.Write(buffer[:n])
			if writeErr != nil {
				err = writeErr
				break
			}
		}
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return Publish(temp, destination)
}
func ExportGIF(ctx context.Context, source, destination string, o GIFOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}
	ffmpeg, err := components.Find("framely-ffmpeg")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "framely-gif-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	palette := filepath.Join(dir, "palette.rgba")
	temp, err := Stage(destination)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	base := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-threads", "2", "-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe", "-ss", strconv.FormatFloat(o.Start, 'f', 3, 64), "-t", strconv.FormatFloat(o.Duration, 'f', 3, 64), "-i", source}
	filter := fmt.Sprintf("fps=%d,scale='min(%d,iw)':-2:flags=lanczos", o.FPS, o.Width)
	// Two passes avoid retaining every source frame while a global palette is built.
	args := append(append([]string{}, base...), "-vf", filter+",palettegen=stats_mode=diff", "-frames:v", "1", "-pix_fmt", "rgba", "-f", "rawvideo", palette)
	// The palette pass decodes the clip once; rendering decodes it again and
	// encodes, so it takes most of the time.
	if err = runProgress(ctx, o, 0, .3, ffmpeg, args...); err != nil {
		return err
	}
	if info, e := os.Stat(palette); e != nil || info.Size() != 16*16*4 {
		return errors.New("视频选段为空或无法生成 GIF 调色板")
	}
	args = append(append([]string{}, base...), "-f", "rawvideo", "-pixel_format", "rgba", "-video_size", "16x16", "-i", palette, "-lavfi", "[0:v]"+filter+"[v];[v][1:v]paletteuse=dither=bayer:diff_mode=rectangle", "-an", "-loop", "0", "-f", "gif", temp)
	if err = runProgress(ctx, o, .3, 1, ffmpeg, args...); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return Publish(temp, destination)
}
func Thumbnail(ctx context.Context, source, destination string) error {
	ffmpeg, err := components.Find("framely-ffmpeg")
	if err != nil {
		return err
	}
	return run(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", source, "-frames:v", "1", "-vf", "scale='min(640,iw)':-2", "-c:v", "bmp", "-update", "1", destination)
}

type limitedLog struct{ bytes.Buffer }

func (b *limitedLog) Write(p []byte) (int, error) {
	n := len(p)
	if room := (16 << 10) - b.Len(); room > 0 {
		_, _ = b.Buffer.Write(p[:min(room, n)])
	}
	return n, nil
}

// runProgress maps FFmpeg's -progress output (out_time_us) onto from..to.
func runProgress(ctx context.Context, o GIFOptions, from, to float64, path string, args ...string) error {
	if o.Progress == nil {
		return run(ctx, path, args...)
	}
	o.Progress(from)
	args = append([]string{"-progress", "pipe:1", "-nostats"}, args...)
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	var log limitedLog
	cmd.Stderr = &log
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		value, ok := strings.CutPrefix(lines.Text(), "out_time_us=")
		if !ok {
			continue
		}
		if us, e := strconv.ParseFloat(value, 64); e == nil && us >= 0 && o.Duration > 0 {
			o.Progress(from + (to-from)*min(1, us/(o.Duration*1e6)))
		}
	}
	_, _ = io.Copy(io.Discard, out)
	if err = cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("媒体导出失败：%w\n%s", err, strings.TrimSpace(log.String()))
	}
	o.Progress(to)
	return nil
}

func run(ctx context.Context, path string, args ...string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	var log limitedLog
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("媒体导出失败：%w\n%s", err, strings.TrimSpace(log.String()))
	}
	return nil
}
