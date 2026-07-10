//go:build windows

package audio

// Default returns the platform recorder: the winmm microphone on Windows.
func Default() Recorder { return NewMic() }
