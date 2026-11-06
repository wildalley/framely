//go:build !linux

package capture

// Close is a no-op on platforms that do not hold a portal connection.
func Close() {}
