package recording

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the test binary as a fake probe so process errors and cancellation are
// exercised on every platform without an installed FFmpeg or shell.
func TestDurationProbeHelperProcess(t *testing.T) {
	mode := os.Getenv("FRAMELY_DURATION_TEST_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "metadata":
		fmt.Fprintln(os.Stderr, "  Duration: 00:00:02.50, start: 0.000000, bitrate: 100 kb/s")
		os.Exit(1)
	case "failed-after-metadata":
		fmt.Fprintln(os.Stderr, "  Duration: 00:00:02.50, start: 0.000000, bitrate: 100 kb/s")
		fmt.Fprintln(os.Stderr, "fatal probe error")
		os.Exit(2)
	case "invalid":
		fmt.Fprintln(os.Stderr, "Invalid data found when processing input")
		os.Exit(1)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(3)
}

func durationTestCommand(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDurationProbeHelperProcess$")
	cmd.Env = append(os.Environ(), "FRAMELY_DURATION_TEST_HELPER="+mode)
	return cmd
}

func TestDurationProbeAcceptsExpectedExitWithMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	duration, err := runDurationProbe(ctx, durationTestCommand(ctx, "metadata"))
	if err != nil || duration != 2.5 {
		t.Fatalf("duration=%v err=%v", duration, err)
	}
}

func TestDurationProbePreservesProcessErrors(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		code   int
		detail string
	}{
		{"failed-after-metadata", 2, "fatal probe error"},
		{"invalid", 1, "Invalid data found when processing input"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			duration, err := runDurationProbe(ctx, durationTestCommand(ctx, tc.mode))
			var exit *exec.ExitError
			if duration != 0 || !errors.As(err, &exit) || exit.ExitCode() != tc.code || !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("duration=%v err=%v", duration, err)
			}
		})
	}
	ctx := context.Background()
	_, err := runDurationProbe(ctx, exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-ffmpeg")))
	// Windows resolves PATHEXT before starting and reports exec.ErrNotFound;
	// Unix starts the absolute path and reports os.ErrNotExist. Keep both causes.
	missing := errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound)
	if !missing || !strings.Contains(err.Error(), "启动视频时长探测失败") {
		t.Fatalf("start failure lost its cause: %v", err)
	}
}

func TestDurationProbeCancelsRunningProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runDurationProbe(ctx, durationTestCommand(ctx, "wait"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost its cause: %v", err)
	}
}
