package recording

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wildalley/framely/internal/capture"
)

type sessionSource struct {
	path, content string
	commands      []string
	commandErrors map[string]error
	status        capture.RecordingStatus
	snapshotError error
	confirm       bool
	closes        int
	onClose       func()
}

func (s *sessionSource) Command(_ context.Context, command string) error {
	s.commands = append(s.commands, command)
	if command == "record" && s.content != "" {
		if err := os.WriteFile(s.path, []byte(s.content), 0600); err != nil {
			return err
		}
	}
	if command == "finish" && s.confirm {
		s.status.Finished = true
	}
	return s.commandErrors[command]
}

func (s *sessionSource) Snapshot() (capture.RecordingStatus, error) {
	return s.status, s.snapshotError
}

func (s *sessionSource) Close() {
	s.closes++
	if s.onClose != nil {
		s.onClose()
	}
}

func newTestSession(t *testing.T) (*Session, *sessionSource) {
	t.Helper()
	session, err := NewSession(filepath.Join(t.TempDir(), "录屏.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	source := &sessionSource{path: session.StagedPath(), content: "recorded bytes", confirm: true,
		status: capture.RecordingStatus{FPS: 60, ElapsedMS: 2000, Frames: 120}}
	return session, source
}

func TestSessionPublishesAfterFinishAndWriterClose(t *testing.T) {
	session, source := newTestSession(t)
	source.onClose = func() {
		// A real source may finish flushing its file only when closed.
		if err := os.WriteFile(source.path, []byte("flushed recording"), 0600); err != nil {
			t.Error(err)
		}
	}
	commands := make(chan string, 3)
	commands <- "pause"
	commands <- "resume"
	commands <- "finish"
	var updates []string
	result, err := session.Run(context.Background(), source, commands, func(update Update) {
		updates = append(updates, update.Command)
	})
	if err != nil || result.Path != session.destination || !result.Status.Finished || source.closes != 1 {
		t.Fatalf("result=%+v closes=%d err=%v", result, source.closes, err)
	}
	if !reflect.DeepEqual(source.commands, []string{"record", "pause", "resume", "finish"}) ||
		!reflect.DeepEqual(updates, []string{"record", "pause", "resume"}) {
		t.Fatalf("commands=%v updates=%v", source.commands, updates)
	}
	session.Close()
	data, err := os.ReadFile(result.Path)
	if err != nil || string(data) != "flushed recording" {
		t.Fatalf("published before source flush: %q %v", data, err)
	}
	if _, err := os.Stat(session.StagedPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file remained after publish: %v", err)
	}
}

func TestSessionPreparationAndPreStartCancellationRemoveStaging(t *testing.T) {
	for _, run := range []bool{false, true} {
		session, source := newTestSession(t)
		if run {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := session.Run(ctx, source, nil, nil)
			if !errors.Is(err, context.Canceled) || len(source.commands) != 0 || source.closes != 1 {
				t.Fatalf("pre-start cancellation: commands=%v closes=%d err=%v", source.commands, source.closes, err)
			}
		}
		session.Close()
		if _, err := os.Stat(session.StagedPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unused staging file retained: %v", err)
		}
	}
}

func TestSessionFailuresRetainBytesAndTheirCause(t *testing.T) {
	cause := errors.New("native writer failed")
	for _, command := range []string{"record", "pause", "finish"} {
		t.Run(command, func(t *testing.T) {
			session, source := newTestSession(t)
			source.commandErrors = map[string]error{command: cause}
			commands := make(chan string, 1)
			if command != "record" {
				commands <- command
			}
			_, err := session.Run(context.Background(), source, commands, nil)
			var recovery *RecoveryError
			if !errors.As(err, &recovery) || !errors.Is(err, cause) || recovery.Path != session.StagedPath() || source.closes != 1 {
				t.Fatalf("recovery=%v closes=%d err=%v", recovery, source.closes, err)
			}
			session.Close()
			if data, err := os.ReadFile(recovery.Path); err != nil || string(data) != source.content {
				t.Fatalf("recorded bytes lost: %q %v", data, err)
			}
			if _, err := os.Stat(session.destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed recording was published: %v", err)
			}
		})
	}
}

func TestSessionFailedStartWithoutBytesDoesNotClaimRecovery(t *testing.T) {
	session, source := newTestSession(t)
	cause := errors.New("encoder unavailable")
	source.content, source.commandErrors = "", map[string]error{"record": cause}
	_, err := session.Run(context.Background(), source, nil, nil)
	var recovery *RecoveryError
	if !errors.Is(err, cause) || errors.As(err, &recovery) {
		t.Fatalf("empty recording claimed recovery: %v", err)
	}
	session.Close()
	if _, err := os.Stat(session.StagedPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty staging file retained: %v", err)
	}
}

func TestSessionRejectsUnconfirmedFinishAndPublishFailure(t *testing.T) {
	for _, publishFailure := range []bool{false, true} {
		session, source := newTestSession(t)
		if publishFailure {
			if err := os.Mkdir(session.destination, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			source.confirm = false
		}
		commands := make(chan string, 1)
		commands <- "finish"
		_, err := session.Run(context.Background(), source, commands, nil)
		var recovery *RecoveryError
		if !errors.As(err, &recovery) || source.closes != 1 {
			t.Fatalf("failed finalization lost recovery: %v", err)
		}
		if !publishFailure && !strings.Contains(err.Error(), "未收到视频保存确认") {
			t.Fatal(err)
		}
		if _, err := os.Stat(recovery.Path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionCancellationClosedControlsAndDeadSourceStop(t *testing.T) {
	for _, mode := range []string{"cancel", "closed-controls", "dead-source"} {
		t.Run(mode, func(t *testing.T) {
			session, source := newTestSession(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			commands := make(chan string)
			cause := errors.New("source disconnected")
			if mode == "closed-controls" {
				close(commands)
			}
			if mode == "dead-source" {
				source.snapshotError = cause
			}
			_, err := session.Run(ctx, source, commands, func(update Update) {
				if mode == "cancel" && update.Command == "record" {
					cancel()
				}
			})
			var recovery *RecoveryError
			if !errors.As(err, &recovery) || source.closes != 1 {
				t.Fatalf("session did not release source and retain bytes: %v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) || mode == "dead-source" && !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
			if mode == "closed-controls" && errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("closed controls kept the session alive")
			}
		})
	}
}

func TestSessionCancellationBeforePublishKeepsTheFragment(t *testing.T) {
	session, source := newTestSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source.onClose = cancel
	commands := make(chan string, 1)
	commands <- "finish"
	_, err := session.Run(ctx, source, commands, nil)
	var recovery *RecoveryError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &recovery) {
		t.Fatalf("publish ignored cancellation: %v", err)
	}
	if _, err := os.Stat(session.destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled finalization published a file: %v", err)
	}
	if _, err := os.Stat(recovery.Path); err != nil {
		t.Fatalf("cancelled finalization lost the fragment: %v", err)
	}
}
