//go:build linux

package capture

// System asks the portal for the desktop's own region selector.
func System(func() []byte) ([]byte, error) { return Screenshot(true) }
