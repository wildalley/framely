//go:build !linux

package shortcuts

func newPlatformProvider() Provider { return nativeProvider{} }
