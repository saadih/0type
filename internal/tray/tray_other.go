//go:build !windows

// Package tray is a no-op off Windows for now.
package tray

// Start is a no-op off Windows.
func Start(tooltip string, openFn, quitFn func()) {}

// Stop is a no-op off Windows.
func Stop() {}
