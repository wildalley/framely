package capture

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strconv"
)

type RecordingOptions struct {
	Path    string
	FPS     int
	MaxEdge int
}
type RecordingStatus struct {
	Active    bool   `json:"active"`
	Paused    bool   `json:"paused"`
	Finished  bool   `json:"finished"`
	ElapsedMS int64  `json:"elapsedMs"`
	Frames    int64  `json:"frames"`
	Captured  int64  `json:"captured"`
	Repeated  int64  `json:"repeated"`
	Missed    int64  `json:"missed"`
	FPS       int    `json:"fps"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Encoder   string `json:"encoder"`
}

func (o RecordingOptions) Arguments() ([]string, error) {
	if !filepath.IsAbs(o.Path) || (o.FPS != 30 && o.FPS != 60) || (o.MaxEdge != 1280 && o.MaxEdge != 1920 && o.MaxEdge != 2560 && o.MaxEdge != 3840) {
		return nil, errors.New("录屏参数无效")
	}
	return []string{"--record", o.Path, "--fps", strconv.Itoa(o.FPS), "--max-edge", strconv.Itoa(o.MaxEdge)}, nil
}
func RecordingCommand(ctx context.Context, source Stream, command string) error {
	s, ok := source.(*processStream)
	if !ok {
		return errors.New("采集组件不支持录屏")
	}
	if command != "record" && command != "pause" && command != "resume" && command != "finish" {
		return errors.New("录屏操作无效")
	}
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	_, err := s.sendCommand(ctx, command)
	if err == nil && command == "record" {
		// Preview IPC stops at this acknowledgment. Drop its last full frame.
		select {
		case <-s.frames:
		default:
		}
	}
	return err
}
func RecordingSnapshot(source Stream) (RecordingStatus, error) {
	s, ok := source.(*processStream)
	if !ok {
		return RecordingStatus{}, errors.New("采集组件不支持录屏")
	}
	select {
	case <-s.done:
		if s.err != nil {
			return RecordingStatus{}, s.err
		}
		return RecordingStatus{}, io.EOF
	default:
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.status, nil
}
