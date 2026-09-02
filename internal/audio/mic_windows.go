//go:build windows

package audio

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	// The capture ring: 8 buffers of 250 ms. The poll loop refills each one as
	// soon as the driver fills it, so the ring holds ~2 s of slack while the
	// recording itself has no length limit.
	numBuffers  = 8
	bufferBytes = BytesPerSecond / 4
	pollEvery   = 20 * time.Millisecond

	waveMapper   = 0xFFFFFFFF // WAVE_MAPPER: let Windows pick the default input
	callbackNull = 0
	whdrDone     = 0x00000001 // WHDR_DONE: the driver has filled this buffer
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

// Mic records the microphone via winmm (waveIn) with no CGO. The driver fills a
// ring of small buffers; a poll goroutine drains each finished buffer into the
// recording, hands it to the streaming callback, and queues it again. There is
// no cap on the recording length.
type Mic struct {
	mu      sync.Mutex
	device  string // selected device name; "" = system default
	onAudio func(pcm []byte)

	// Capture state, valid between Start and Stop.
	pin  runtime.Pinner
	hwi  uintptr
	hdrs []wavehdr // one per ring buffer, pinned for the driver
	bufs [][]byte
	next int    // ring index the driver fills next (buffers complete in order)
	rec  []byte // everything captured so far
	stop chan struct{}
	done chan struct{}
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

// OnAudio registers the streaming callback (see Streaming). It applies on the
// next Start.
func (m *Mic) OnAudio(fn func(pcm []byte)) {
	m.mu.Lock()
	m.onAudio = fn
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
		nSamplesPerSec:  SampleRate,
		nAvgBytesPerSec: BytesPerSecond,
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

	// The driver writes to the buffers and headers asynchronously through the
	// raw pointers we hand it, so none of them may move or be collected while
	// capture runs. Pinning one element pins its whole allocation.
	m.hdrs = make([]wavehdr, numBuffers)
	m.bufs = make([][]byte, numBuffers)
	m.pin.Pin(&m.hdrs[0])
	for i := range m.bufs {
		m.bufs[i] = make([]byte, bufferBytes)
		m.pin.Pin(&m.bufs[i][0])
		m.hdrs[i] = wavehdr{lpData: uintptr(unsafe.Pointer(&m.bufs[i][0])), dwBufferLength: bufferBytes}
	}
	hdrSize := unsafe.Sizeof(m.hdrs[0])
	fail := func(stage string, r uintptr) error {
		procWaveInReset.Call(hwi)
		for i := range m.hdrs {
			procWaveInUnprepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdrs[i])), hdrSize)
		}
		procWaveInClose.Call(hwi)
		m.pin.Unpin()
		m.hdrs, m.bufs = nil, nil
		return fmt.Errorf("%s: %w", stage, mmErr(r))
	}
	for i := range m.hdrs {
		if r, _, _ := procWaveInPrepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdrs[i])), hdrSize); r != 0 {
			return fail("waveInPrepareHeader", r)
		}
		if r, _, _ := procWaveInAddBuffer.Call(hwi, uintptr(unsafe.Pointer(&m.hdrs[i])), hdrSize); r != 0 {
			return fail("waveInAddBuffer", r)
		}
	}
	if r, _, _ := procWaveInStart.Call(hwi); r != 0 {
		return fail("waveInStart", r)
	}
	m.hwi = hwi
	m.next = 0
	m.rec = m.rec[:0]
	m.stop = make(chan struct{})
	m.done = make(chan struct{})
	go m.poll(hwi, m.onAudio, m.stop, m.done)
	return nil
}

// poll drains finished ring buffers in order and requeues them until stop is
// closed. It owns hdrs/bufs/rec/next while it runs; Stop joins it before
// touching them again.
func (m *Mic) poll(hwi uintptr, onAudio func([]byte), stop, done chan struct{}) {
	defer close(done)
	hdrSize := unsafe.Sizeof(m.hdrs[0])
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		for {
			h := &m.hdrs[m.next]
			if atomic.LoadUint32(&h.dwFlags)&whdrDone == 0 {
				break
			}
			m.harvest(m.next, onAudio)
			// Hand the buffer straight back to the driver: it stays prepared,
			// and waveInAddBuffer clears WHDR_DONE as it requeues it.
			procWaveInAddBuffer.Call(hwi, uintptr(unsafe.Pointer(h)), hdrSize)
			m.next = (m.next + 1) % numBuffers
		}
	}
}

// harvest appends ring buffer i's audio to the recording and streams it.
func (m *Mic) harvest(i int, onAudio func([]byte)) {
	h := &m.hdrs[i]
	n := int(h.dwBytesRecorded)
	h.dwBytesRecorded = 0
	if n == 0 {
		return
	}
	data := m.bufs[i][:n]
	if onAudio == nil {
		m.rec = append(m.rec, data...)
		return
	}
	// A streaming consumer owns the audio; keeping a second copy here would
	// grow without bound over a long live dictation.
	chunk := make([]byte, n)
	copy(chunk, data)
	onAudio(chunk)
}

// Stop ends capture and returns the recording as a WAV file. With a streaming
// callback set, the audio went to the callback instead (the last ring buffers
// are flushed through it here, before Stop returns) and the WAV is empty.
func (m *Mic) Stop() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hwi == 0 {
		return nil, fmt.Errorf("not recording")
	}
	hwi := m.hwi
	hdrSize := unsafe.Sizeof(m.hdrs[0])

	close(m.stop)
	<-m.done
	procWaveInStop.Call(hwi)
	procWaveInReset.Call(hwi) // returns every queued buffer, marked done, in order
	for range m.hdrs {
		if atomic.LoadUint32(&m.hdrs[m.next].dwFlags)&whdrDone != 0 {
			m.harvest(m.next, m.onAudio)
		}
		m.next = (m.next + 1) % numBuffers
	}
	for i := range m.hdrs {
		procWaveInUnprepareHeader.Call(hwi, uintptr(unsafe.Pointer(&m.hdrs[i])), hdrSize)
	}
	procWaveInClose.Call(hwi)
	m.pin.Unpin()

	wav := EncodeWAV(m.rec)
	m.rec = nil
	m.hdrs, m.bufs = nil, nil
	m.hwi = 0
	return wav, nil
}
