// Package app is the shared dictation engine used by both the console
// (cmd/0type) and the Wails GUI: hold a trigger, record the mic, transcribe,
// clean, and paste — one ordered output at a time.
package app

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/saadih/0type/internal/audio"
	"github.com/saadih/0type/internal/cleanup"
	"github.com/saadih/0type/internal/hotkey"
	"github.com/saadih/0type/internal/inject"
	"github.com/saadih/0type/internal/models"
	"github.com/saadih/0type/internal/overlay"
	"github.com/saadih/0type/internal/transcribe"
)

// minRecordingBytes ignores fat-finger taps: below ~0.15s of 16 kHz mono 16-bit
// audio there is no speech, only a wasted transcription round-trip.
const minRecordingBytes = 44 + 16000*2*15/100

// Config selects the backends and the initial trigger binding.
type Config struct {
	GroqAPIKey  string // cloud transcription; empty -> local Parakeet or (if AllowStub) the stub
	CleanupURL  string // local LLM base URL; empty -> auto-start bundled server if the model is present
	Binding     hotkey.Binding
	Mode        string // "hold" (default) | "toggle"
	InputDevice string // microphone name; empty -> system default
	AllowStub   bool   // console/dev: fall back to the canned stub transcript when nothing else is available
	// Notify (may be nil) surfaces user-facing messages to a UI. kind is
	// "info" or "error".
	Notify func(kind, msg string)
}

// Engine wires the dictation pipeline together and runs it.
type Engine struct {
	rec     audio.Recorder
	inj     inject.Injector
	trig    hotkey.Controller
	onState func(recording bool)
	events  chan bool
	jobs    chan []byte

	asrMu sync.Mutex // guards asr (hot-swapped when Parakeet is downloaded)
	asr   transcribe.Transcriber

	cleanMu    sync.Mutex // guards clean and srv
	clean      cleanup.Cleaner
	cleanupSet bool // an explicit cleanup URL was configured
	srv        *models.Server
	srvMu      sync.Mutex // serializes local cleanup-server startup

	toggleMode atomic.Bool            // true -> tap to toggle; false -> hold to talk
	notify     func(kind, msg string) // optional UI notifier; set once before Start

	ovMu       sync.Mutex // guards the overlay state below
	recording  bool
	processing int // in-flight jobs being transcribed/cleaned
}

// New builds an engine from cfg. onState (may be nil) is called with true when a
// recording starts and false when it stops — an extra hook for UIs beyond the
// built-in floating overlay.
func New(cfg Config, onState func(recording bool)) *Engine {
	// Base backend: cloud if a key is set, else the canned stub for console/dev,
	// else a placeholder that reports ErrNoModel so the GUI prompts for a
	// download instead of pasting fake text.
	var asr transcribe.Transcriber
	switch {
	case cfg.GroqAPIKey != "":
		asr = transcribe.NewGroq(cfg.GroqAPIKey)
	case cfg.AllowStub:
		asr = transcribe.NewStub()
	default:
		asr = transcribe.NewNeedModel()
	}
	// Prefer local Parakeet when the model is downloaded and it's compiled in
	// (built with -tags parakeet); NewParakeet errors on the stub build.
	if models.Parakeet().Installed() {
		if dir, err := models.ExtractParakeet(); err == nil {
			if p, err := transcribe.NewParakeet(dir); err == nil {
				asr = p
			}
		}
	}
	var clean cleanup.Cleaner = cleanup.NewNoop()
	if cfg.CleanupURL != "" {
		clean = cleanup.NewLLM(cfg.CleanupURL)
	}
	if onState == nil {
		onState = func(bool) {}
	}
	b := cfg.Binding
	if !b.Valid() {
		b = hotkey.DefaultBinding()
	}
	rec := audio.Default()
	if ds, ok := rec.(audio.DeviceSelector); ok {
		ds.SetInputDevice(cfg.InputDevice)
	}
	e := &Engine{
		rec:        rec,
		asr:        asr,
		inj:        inject.Default(),
		trig:       hotkey.New(b),
		onState:    onState,
		events:     make(chan bool, 16),
		jobs:       make(chan []byte, 8),
		clean:      clean,
		cleanupSet: cfg.CleanupURL != "",
		notify:     cfg.Notify,
	}
	e.toggleMode.Store(cfg.Mode == "toggle")
	return e
}

// Start launches the engine's goroutines, the floating overlay, the bundled
// cleanup server (if applicable), and the global trigger.
func (e *Engine) Start() error {
	overlay.Start()
	e.maybeStartLocalServer()
	go e.run()
	go e.processLoop()
	go func() { _, _ = e.cleaner().Clean("warm up") }() // prime the cleanup prompt cache
	return e.trig.Start(e.onPress, e.onRelease)
}

// cleaner returns the current cleaner under lock.
func (e *Engine) cleaner() cleanup.Cleaner {
	e.cleanMu.Lock()
	defer e.cleanMu.Unlock()
	return e.clean
}

// transcriber returns the current transcriber under lock.
func (e *Engine) transcriber() transcribe.Transcriber {
	e.asrMu.Lock()
	defer e.asrMu.Unlock()
	return e.asr
}

// EnableLocalTranscription loads the downloaded Parakeet model and swaps the
// transcriber in live, so a fresh download takes effect without a restart. It
// errors if the model isn't present or this build was compiled without local
// transcription (no -tags parakeet).
func (e *Engine) EnableLocalTranscription() error {
	if !models.Parakeet().Installed() {
		return fmt.Errorf("parakeet model not installed")
	}
	dir, err := models.ExtractParakeet()
	if err != nil {
		return err
	}
	p, err := transcribe.NewParakeet(dir)
	if err != nil {
		return err // stub build: NewParakeet always errors
	}
	e.asrMu.Lock()
	e.asr = p
	e.asrMu.Unlock()
	return nil
}

// emit sends a user-facing message to the UI notifier, if one was set.
func (e *Engine) emit(kind, msg string) {
	if e.notify != nil {
		e.notify(kind, msg)
	}
}

// SetMode switches between hold ("hold") and tap-to-toggle ("toggle") live.
func (e *Engine) SetMode(mode string) { e.toggleMode.Store(mode == "toggle") }

// SetInputDevice selects the microphone by name ("" = system default). It takes
// effect on the next recording.
func (e *Engine) SetInputDevice(name string) {
	if ds, ok := e.rec.(audio.DeviceSelector); ok {
		ds.SetInputDevice(name)
	}
}

// SetCleanupURL swaps the cleanup backend live and pre-warms it.
func (e *Engine) SetCleanupURL(url string) {
	e.cleanMu.Lock()
	if url == "" {
		e.clean = cleanup.NewNoop()
	} else {
		e.clean = cleanup.NewLLM(url)
	}
	c := e.clean
	e.cleanMu.Unlock()
	go func() { _, _ = c.Clean("warm up") }()
}

// EnableLocalCleanup starts the bundled llama-server for the downloaded Qwen
// model and swaps the cleaner to it live, so a fresh download takes effect
// without a restart. It is a no-op when an explicit cleanup URL was configured
// or a local server is already running, and it stores the server so Stop can
// shut it down.
func (e *Engine) EnableLocalCleanup() error {
	e.srvMu.Lock()
	defer e.srvMu.Unlock()
	e.cleanMu.Lock()
	skip := e.cleanupSet || e.srv != nil
	e.cleanMu.Unlock()
	if skip {
		return nil
	}
	srv, url, err := models.StartLlama()
	if err != nil {
		return err
	}
	e.cleanMu.Lock()
	e.srv = srv
	e.cleanMu.Unlock()
	e.SetCleanupURL(url)
	return nil
}

// maybeStartLocalServer brings up local cleanup at startup when the Qwen model
// is already downloaded and no explicit cleanup URL was configured.
func (e *Engine) maybeStartLocalServer() {
	if e.cleanupSet || !models.Qwen().Installed() {
		return
	}
	go func() {
		if err := e.EnableLocalCleanup(); err != nil {
			log.Printf("local cleanup server: %v", err)
			return
		}
		log.Printf("local cleanup ready")
	}()
}

// Rebind captures the next key/button the user presses, applies it live, and
// returns the new binding.
func (e *Engine) Rebind() (hotkey.Binding, error) {
	b, err := e.trig.Capture()
	if err != nil {
		return b, err
	}
	e.trig.SetBinding(b)
	return b, nil
}

// Stop shuts down the bundled cleanup server if the engine started one.
func (e *Engine) Stop() {
	e.cleanMu.Lock()
	srv := e.srv
	e.cleanMu.Unlock()
	if srv != nil {
		srv.Stop()
	}
}

func (e *Engine) onPress()   { e.signal(true) }
func (e *Engine) onRelease() { e.signal(false) }

// signal runs on the hook thread; never block it (a stalled low-level hook
// freezes the whole system's mouse). Drop on overflow instead.
func (e *Engine) signal(press bool) {
	select {
	case e.events <- press:
	default:
		log.Printf("hotkey: event queue full, dropped an event")
	}
}

// setRecording updates the recording flag and refreshes the overlay dot.
func (e *Engine) setRecording(on bool) {
	e.ovMu.Lock()
	e.recording = on
	m := e.overlayMode()
	e.ovMu.Unlock()
	overlay.Show(m)
}

// addProcessing adjusts the in-flight job count and refreshes the overlay dot.
func (e *Engine) addProcessing(delta int) {
	e.ovMu.Lock()
	e.processing += delta
	m := e.overlayMode()
	e.ovMu.Unlock()
	overlay.Show(m)
}

// overlayMode maps engine state to a dot color. Recording wins over processing,
// so a fresh dictation shows red even while the previous one is still cleaning
// up. Assumes ovMu is held.
func (e *Engine) overlayMode() overlay.Mode {
	switch {
	case e.recording:
		return overlay.Recording
	case e.processing > 0:
		return overlay.Processing
	default:
		return overlay.Hidden
	}
}

func (e *Engine) run() {
	capturing := false // current state, so toggle mode knows when to stop
	for press := range e.events {
		if e.toggleMode.Load() {
			if !press {
				continue // toggle reacts to the press, not the release
			}
			if capturing {
				capturing = false
				e.stopCapture()
			} else {
				capturing = e.startCapture()
			}
			continue
		}
		// hold to talk
		if press {
			capturing = e.startCapture()
		} else if capturing {
			capturing = false
			e.stopCapture()
		}
	}
}

// startCapture shows the dot and opens the mic. It returns false (and stays
// idle) if the mic can't be opened.
func (e *Engine) startCapture() bool {
	e.setRecording(true)
	e.onState(true)
	if err := e.rec.Start(); err != nil {
		log.Printf("record start: %v", err)
		e.emit("error", "Could not open the microphone.")
		e.onState(false)
		e.setRecording(false)
		return false
	}
	return true
}

// stopCapture ends the recording and hands it to the output worker.
func (e *Engine) stopCapture() {
	e.onState(false)
	wav, err := e.rec.Stop()
	if err != nil {
		e.setRecording(false)
		log.Printf("record stop: %v", err)
		return
	}
	if len(wav) < minRecordingBytes {
		e.setRecording(false) // too short to be speech — ignore the tap
		return
	}
	// Mark processing before clearing recording so the dot goes red -> blue with
	// no hidden flicker; the output worker clears it when done.
	e.addProcessing(1)
	e.setRecording(false)
	e.jobs <- wav
}

// processLoop is the single ordered output worker: transcribe -> clean -> inject,
// one recording at a time, so overlapping dictations never corrupt the clipboard.
func (e *Engine) processLoop() {
	for wav := range e.jobs {
		e.process(wav)
	}
}

func (e *Engine) process(wav []byte) {
	defer e.addProcessing(-1) // clear the blue dot when this job finishes
	raw, err := e.transcriber().Transcribe(wav)
	if err != nil {
		if errors.Is(err, transcribe.ErrNoModel) {
			e.emit("error", "Download a transcription model to start dictating.")
		} else {
			log.Printf("transcribe: %v", err)
			e.emit("error", "Transcription failed.")
		}
		return
	}
	text, err := e.cleaner().Clean(raw)
	if err != nil {
		log.Printf("cleanup: %v", err)
		e.emit("info", "Cleanup unavailable; pasted the raw transcript.")
		text = raw // fall back to the raw transcript rather than dropping it
	}
	if text == "" {
		if raw != "" {
			log.Printf("cleanup returned empty; nothing pasted (if unexpected, start the cleanup server with --jinja)")
		}
		return // nothing to paste (silence, or a filler-only utterance)
	}
	if err := e.inj.Inject(text); err != nil {
		log.Printf("inject: %v", err)
	}
}
