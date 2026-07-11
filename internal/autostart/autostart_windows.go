//go:build windows

// Package autostart toggles whether 0type launches at Windows login by writing
// a per-user entry under the HKCU Run key. Per-user needs no admin rights.
package autostart

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "0type"
)

// Enabled reports whether the Run entry is present.
func Enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(valueName)
	return err == nil
}

// SetEnabled adds (on) or removes (off) the Run entry, pointing at the current
// executable.
func SetEnabled(on bool) error {
	if !on {
		return remove()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName, `"`+exe+`"`) // quoted so a spaced path still parses
}

func remove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
