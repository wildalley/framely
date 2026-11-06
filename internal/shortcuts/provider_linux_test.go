//go:build linux

package shortcuts

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestPortalMustAssignKeysToTheCaptureAction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result map[string]dbus.Variant
		valid  bool
	}{
		{"assigned", map[string]dbus.Variant{"shortcuts": dbus.MakeVariant([]portalShortcut{{ID: "capture", Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Super+Shift+S")}}})}, true},
		{"missing", nil, false},
		{"empty", map[string]dbus.Variant{"shortcuts": dbus.MakeVariant([]portalShortcut{{ID: "capture", Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("")}}})}, false},
		{"wrong-id", map[string]dbus.Variant{"shortcuts": dbus.MakeVariant([]portalShortcut{{ID: "other", Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Super+S")}}})}, false},
		{"malformed", map[string]dbus.Variant{"shortcuts": dbus.MakeVariant("not a shortcut list")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			description, err := boundDescription(tc.result)
			if (err == nil) != tc.valid || (tc.valid && description == "") {
				t.Fatalf("description=%q, err=%v", description, err)
			}
		})
	}
}
