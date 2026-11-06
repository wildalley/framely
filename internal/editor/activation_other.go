//go:build !linux

package editor

func withActivationToken(_ string, fn func()) { fn() }
