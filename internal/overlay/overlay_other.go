//go:build !windows

// Package overlay's recording pill is Windows-only for now; these no-ops keep
// the engine building on other platforms.
package overlay

// Start is a no-op off Windows.
func Start() {}

// Show is a no-op off Windows.
func Show(recording bool) {}
