package desktop

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/editor"
)

// Keep recording history separate from the settings draft, so saving an open
// settings window cannot overwrite a newly remembered recording region.
type recordingSetup struct {
	Selection *editor.ScreenSelection `json:"selection,omitempty"`
	FPS       string                  `json:"fps"`
	Quality   string                  `json:"quality"`
}

func (s recordingSetup) normalized() recordingSetup {
	if s.FPS != "30" && s.FPS != "60" {
		s.FPS = "60"
	}
	switch s.Quality {
	case "720p", "1080p", "1440p", "4K":
	default:
		s.Quality = "1080p"
	}
	if s.Selection != nil {
		if _, err := s.Selection.ForStream(s.Selection.Size); err != nil || s.Selection.Display.Width <= 0 || s.Selection.Display.Height <= 0 {
			s.Selection = nil
		}
	}
	return s
}

func recordingSetupPath() (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "recording.json"), nil
}

func loadRecordingSetup() recordingSetup {
	path, err := recordingSetupPath()
	var setup recordingSetup
	if err == nil {
		if data, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(data, &setup) != nil {
				setup = recordingSetup{}
			}
		}
	}
	return setup.normalized()
}

func saveRecordingSetup(setup recordingSetup) error {
	path, err := recordingSetupPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(setup.normalized())
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".recording-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// A fresh stream must still identify the same monitor. Never apply a remembered
// monitor crop to a window, a disconnected monitor, or a changed aspect ratio.
func reuseRecordingRegion(selection *editor.ScreenSelection, screen *mygo.Display, size image.Point) (image.Rectangle, bool) {
	if selection == nil || screen == nil || screen.Bounds != selection.Display ||
		int64(size.X)*int64(selection.Size.Y) != int64(size.Y)*int64(selection.Size.X) {
		return image.Rectangle{}, false
	}
	region, err := selection.ForStream(size)
	return region, err == nil
}
