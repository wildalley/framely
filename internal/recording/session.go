package recording

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/wildalley/framely/internal/capture"
)

// Source is the recording protocol, separate from preview frames and windows.
type Source interface {
	Command(context.Context, string) error
	Snapshot() (capture.RecordingStatus, error)
	Close()
}

type Update struct {
	Command string // "record", "pause", "resume", or empty for a status poll
	Status  capture.RecordingStatus
}

type Result struct {
	Path   string
	Status capture.RecordingStatus
}

// RecoveryError identifies an interrupted, nonempty file that remains usable
// for recovery. Unwrap retains the protocol, cancellation or filesystem cause.
type RecoveryError struct {
	Path string
	Err  error
}

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("录屏中断，视频片段保留于 %s\n%v", e.Path, e.Err)
}

func (e *RecoveryError) Unwrap() error { return e.Err }

// Session owns one recording's staging file. Run and Close belong to the same
// worker goroutine; defer Close immediately after NewSession succeeds.
type Session struct {
	destination, staged string
	preserve, used      bool
}

func NewSession(destination string) (*Session, error) {
	staged, err := Stage(destination)
	if err != nil {
		return nil, err
	}
	return &Session{destination: destination, staged: staged}, nil
}

func (s *Session) StagedPath() string { return s.staged }

// Close removes unused/empty staging files, retaining nonempty interrupted
// recordings. It is safe to call again after a successful publish.
func (s *Session) Close() {
	if s.preserve {
		info, err := os.Stat(s.staged)
		if err != nil || info.Size() > 0 {
			return
		}
	}
	_ = os.Remove(s.staged)
}

// Run takes ownership of source, starts recording, handles controls, and
// publishes only after the worker confirms finish. Updates run synchronously
// on this goroutine; a window must marshal them back to its UI thread.
func (s *Session) Run(ctx context.Context, source Source, commands <-chan string, update func(Update)) (Result, error) {
	closeSource := sync.OnceFunc(source.Close)
	defer closeSource()
	fail := func(err error) (Result, error) {
		// Flush/close the source before deciding whether a recoverable file
		// exists, including a record command that wrote bytes but failed.
		closeSource()
		if s.preserve {
			if info, statErr := os.Stat(s.staged); statErr == nil && info.Size() > 0 {
				return Result{}, &RecoveryError{Path: s.staged, Err: err}
			}
		}
		return Result{}, err
	}
	if s.used {
		return fail(errors.New("录屏会话已使用"))
	}
	s.used = true
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	s.preserve = true // A failed acknowledgment may still leave recorded bytes.
	if err := source.Command(ctx, "record"); err != nil {
		return fail(fmt.Errorf("启动录屏失败：%w", err))
	}
	publish := func(next Update) {
		if update != nil {
			update(next)
		}
	}
	publish(Update{Command: "record"})
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case command, ok := <-commands:
			if !ok {
				return fail(errors.New("录屏控制已关闭"))
			}
			if command != "pause" && command != "resume" && command != "finish" {
				return fail(errors.New("录屏操作无效"))
			}
			if err := source.Command(ctx, command); err != nil {
				return fail(fmt.Errorf("录屏控制失败：%w", err))
			}
			if command != "finish" {
				publish(Update{Command: command})
				continue
			}
			status, err := source.Snapshot()
			if err != nil {
				return fail(fmt.Errorf("读取视频保存确认失败：%w", err))
			}
			if !status.Finished {
				return fail(errors.New("未收到视频保存确认"))
			}
			closeSource() // Release the writer's handle before rename on Windows.
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			if err := Publish(s.staged, s.destination); err != nil {
				return fail(fmt.Errorf("保存视频失败：%w", err))
			}
			return Result{Path: s.destination, Status: status}, nil
		case <-ticker.C:
			status, err := source.Snapshot()
			if err != nil {
				return fail(fmt.Errorf("读取录屏状态失败：%w", err))
			}
			publish(Update{Status: status})
		}
	}
}
