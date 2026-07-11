//go:build windows

package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	sampleRate    = 16000
	channels      = 1
	bitsPerSample = 16
	maxSeconds    = 120 // fixed capture buffer: plenty for a dictation
	maxBytes      = sampleRate * channels * (bitsPerSample / 8) * maxSeconds

	waveMapper    = 0xFFFFFFFF // WAVE_MAPPER: let Windows pick the default input
	callbackNull  = 0
	waveFormatPCM = 1
)

var (
	winmm = syscall.NewLazyDLL("winmm.dll")

	procWaveInOpen            = winmm.NewProc("waveInOpen")
	procWaveInPrepareHeader   = winmm.NewProc("waveInPrepareHeader")
	procWaveInAddBuffer       = winmm.NewProc("waveInAddBuffer")
	procWaveInStart           = winmm.NewProc("waveInStart")
	procWaveInStop            = winmm.NewProc("waveInStop")
	procWaveInReset           = winmm.NewProc("waveInReset")
	procWaveInUnprepareHeader = winmm.NewProc("waveInUnprepareHeader")
	procWaveInClose           = winmm.NewProc("waveInClose")
	procWaveInGetNumDevs      = winmm.NewProc("waveInGetNumDevs")
	procWaveInGetDevCapsW     = winmm.NewProc("waveInGetDevCapsW")
)

type waveformatex struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type wavehdr struct {
	lpData          uintptr
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

// waveincaps mirrors WAVEINCAPSW; szPname holds the device's display name.
type waveincaps struct {
	wMid           uint16
	wPid           uint16
	vDriverVersion uint32
	szPname        [32]uint16
	dwFormats      uint32
	wChannels      uint16
	wReserved1     uint16
}

// InputDevices lists the names of the available microphones, in winmm device
// order. The names match what SetInputDevice expects.
func InputDevices() []string {
	n, _, _ := procWaveInGetNumDevs.Call()
	names := make([]string, 0, int(n))
	for i := uintptr(0); i < n; i++ {
		var caps waveincaps
		if r, _, _ := procWaveInGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); r == 0 {
			names = append(names, syscall.UTF16ToString(caps.szPname[:]))
		}
	}
	return names
}

// Mic records the default microphone via winmm (waveIn) with no CGO. It captures
// into a single fixed buffer between Start and Stop — simple and robust, enough
// for a dictation up to maxSeconds.
type Mic struct {
	mu     sync.Mutex
	pin    runtime.Pinner
	hwi    uintptr
	hdr    wavehdr
	buf    []byte
	device string // selected device name; "" = system default
}

// NewMic returns a winmm microphone recorder.
func NewMic() *Mic { return &Mic{} }

// SetInputDevice picks the microphone by name ("" = system default). It applies
// on the next Start.
func (m *Mic) SetInputDevice(name string) {
	m.mu.Lock()
	m.device = name
	m.mu.Unlock()
}

// deviceID resolves the selected device name to a winmm device index, falling
// back to WAVE_MAPPER (the default) when unset or not found. The caller holds
// m.mu (Start does).
func (m *Mic) deviceID() uintptr {
	if m.device == "" {
		return waveMapper
	}
	n, _, _ := procWaveInGetNumDevs.Call()
	for i := uintptr(0); i < n; i++ {
		var caps waveincaps
		if r, _, _ := procWaveInGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); r == 0 {
			if syscall.UTF16ToString(caps.szPname[:]) == m.device {
				return i
			}
		}
	}
	return waveMapper
}

func mmErr(code uintptr) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("winmm error %d", code)
}

// Start opens the microphone and begins capturing.
func (m *Mic) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hwi != 0 {
		return fmt.Errorf("already recording")
	}

	wf := waveformatex{
		wFormatTag:      waveFormatPCM,
		nChannels:       channels,
		nSamplesPerSec:  sampleRate,
		nAvgBytesPerSec: sampleRate * channels * (bitsPerSample / 8),
		nBlockAlign:     channels * (bitsPerSample / 8),
		wBitsPerSample:  bitsPerSample,
	}
	var hwi uintptr
	if r, _, _ := procWaveInOpen.Call(
		uintptr(unsafe.Pointer(&hwi)), m.deviceID(),
		uintptr(unsafe.Pointer(&wf)), callbackNull, 0, callbackNull,
	); r != 0 {
		return fmt.Errorf("waveInOpen: %w", mmErr(r))
	}

	m.buf = make([]byte, maxBytes)
	// The audio driver writes to the buffer and header asynchronously through the
	// raw pointers we hand it, so they must not move or be collected mid-capture.
	m.pin.Pin(&m.buf[0])
	m.hdr = wavehdr{lpData: uintptr(unsafe.Pointer(&m.buf[0])), dwBufferLength: uint32(len(m.buf))}
	m.pin.Pin(&m.hdr)

	hdrSize := unsafe.Sizeof(m.hdr)
	if r, _, _ := procWaveInPrepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdr)), hdrSize); r != 0 {
		m.pin.Unpin()
		procWaveInClose.Call(hwi)
		return fmt.Errorf("waveInPrepareHeader: %w", mmErr(r))
	}
	if r, _, _ := procWaveInAddBuffer.Call(hwi, uintptr(unsafe.Pointer(&m.hdr)), hdrSize); r != 0 {
		procWaveInUnprepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdr)), hdrSize)
		m.pin.Unpin()
		procWaveInClose.Call(hwi)
		return fmt.Errorf("waveInAddBuffer: %w", mmErr(r))
	}
	if r, _, _ := procWaveInStart.Call(hwi); r != 0 {
		procWaveInUnprepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdr)), hdrSize)
		m.pin.Unpin()
		procWaveInClose.Call(hwi)
		return fmt.Errorf("waveInStart: %w", mmErr(r))
	}
	m.hwi = hwi
	return nil
}

// Stop ends capture and returns the recording as a WAV file.
func (m *Mic) Stop() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hwi == 0 {
		return nil, fmt.Errorf("not recording")
	}
	hwi := m.hwi
	hdrSize := unsafe.Sizeof(m.hdr)

	procWaveInStop.Call(hwi)
	procWaveInReset.Call(hwi) // returns the pending buffer and sets dwBytesRecorded
	n := m.hdr.dwBytesRecorded
	procWaveInUnprepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdr)), hdrSize)
	procWaveInClose.Call(hwi)

	wav := encodeWAV(m.buf[:n])
	m.pin.Unpin()
	m.buf = nil
	m.hwi = 0
	return wav, nil
}

// encodeWAV wraps 16 kHz mono 16-bit PCM in a minimal RIFF/WAVE container.
func encodeWAV(pcm []byte) []byte {
	var b bytes.Buffer
	dataLen := uint32(len(pcm))
	blockAlign := uint16(channels * (bitsPerSample / 8))
	byteRate := uint32(sampleRate) * uint32(blockAlign)

	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(waveFormatPCM))
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, byteRate)
	binary.Write(&b, binary.LittleEndian, blockAlign)
	binary.Write(&b, binary.LittleEndian, uint16(bitsPerSample))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, dataLen)
	b.Write(pcm)
	return b.Bytes()
}
