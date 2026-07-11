//go:build !windows

// Package overlay's recording pill is Windows-only for now; these no-ops keep
// the engine building on other platforms.
package overlay

// Mode selects what the dot shows (see the Windows build for the real thing).
type Mode uintptr

const (
	Hidden     Mode = 0
	Recording  Mode = 1
	Processing Mode = 2
)

// Start is a no-op off Windows.
func Start() {}

// Show is a no-op off Windows.
func Show(m Mode) {}
