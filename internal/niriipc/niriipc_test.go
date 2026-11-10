package niriipc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestPlaceFloatsMovesAndRestoresFocus(t *testing.T) {
	var calls []string
	calls0 := 0
	run = func(_ context.Context, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		if line == "--json windows" {
			calls0++
			if calls0 < 3 { // The control window maps a little later.
				return []byte(`[{"id":7,"title":"page","pid":1,"is_focused":true}]`), nil
			}
			return []byte(fmt.Sprintf(`[{"id":7,"title":"page","pid":1,"is_focused":true},{"id":9,"title":"bar","pid":%d}]`, os.Getpid())), nil
		}
		calls = append(calls, line)
		return nil, nil
	}
	if err := Place(context.Background(), "bar", 120, 640); err != nil {
		t.Fatal(err)
	}
	want := []string{"action toggle-window-floating --id 9", "action move-floating-window --id 9 -x 120 -y 640", "action focus-window --id 7"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls: %q", calls)
	}
}
