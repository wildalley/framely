package desktop

import (
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
		{"show", nil, Launch{}},
		{"hidden", []string{"--hidden"}, Launch{Hidden: true}},
		{"capture", []string{"--capture"}, Launch{Capture: true}},
		{"capture-from-hidden", []string{"--hidden", "--capture"}, Launch{Hidden: true, Capture: true}},
		{"file", []string{"--hidden", "my project.screenpin"}, Launch{Hidden: true, File: "my project.screenpin"}},
		{"version", []string{"--version"}, Launch{Version: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseArgs(tc.args)
			if err != nil || got != tc.want {
				t.Fatalf("launch=%+v, err=%v", got, err)
			}
		})
	}
	for _, args := range [][]string{{"--unknown"}, {"a.png", "b.png"}, {"--capture", "a.png"}} {
		if _, err := ParseArgs(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if _, err := ParseArgs([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}
