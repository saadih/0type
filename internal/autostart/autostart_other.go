//go:build !windows

// Package autostart is a no-op off Windows for now.
package autostart

// Enabled always reports false off Windows.
func Enabled() bool { return false }

// SetEnabled is a no-op off Windows.
func SetEnabled(on bool) error { return nil }
