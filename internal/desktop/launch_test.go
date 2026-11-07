package desktop

import (
	"context"
	"errors"
	"flag"
	"testing"
)

func TestLaunchArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want Launch
	}{
		{"tray", nil, Launch{}},
		{"hidden", []string{"--hidden"}, Launch{Hidden: true}},
		{"capture", []string{"--capture"}, Launch{Capture: true}},
		{"scroll", []string{"--scroll"}, Launch{Scroll: true}},
		{"capture-from-hidden", []string{"--hidden", "--capture"}, Launch{Hidden: true, Capture: true}},
		{"file", []string{"--hidden", "my image.png"}, Launch{Hidden: true, File: "my image.png"}},
		{"version", []string{"--version"}, Launch{Version: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseArgs(tc.args)
			if err != nil || got != tc.want {
				t.Fatalf("launch=%+v, err=%v", got, err)
			}
		})
	}
	for _, args := range [][]string{{"--unknown"}, {"a.png", "b.png"}, {"--capture", "a.png"}, {"--scroll", "a.png"}, {"--capture", "--scroll"}} {
		if _, err := ParseArgs(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if _, err := ParseArgs([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

func TestRecordingDoesNotTriggerACapture(t *testing.T) {
	s := &application{ctx: context.Background(), settings: &settingsWindow{recording: true}}
	s.CaptureWithActivation("recorded-key")
	if s.captureBusy || s.operations != 0 {
		t.Fatal("recording the current shortcut started a screenshot")
	}
}
