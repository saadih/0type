// Package app is the shared dictation engine used by both the console
// (cmd/0type) and the Wails GUI: hold a trigger, record the mic, transcribe,
// clean, and paste — one ordered output at a time.
package app

import (
	"log"
	"sync"

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
	GroqAPIKey string // cloud transcription; empty -> stub transcript
	CleanupURL string // local LLM base URL; empty -> auto-start bundled server if the model is present
	Binding    hotkey.Binding
}

// Engine wires the dictation pipeline together and runs it.
type Engine struct {
	rec     audio.Recorder
	asr     transcribe.Transcriber
	inj     inject.Injector
	trig    hotkey.Controller
	onState func(recording bool)
	events  chan bool
	jobs    chan []byte

	cleanMu    sync.Mutex // guards clean and srv
	clean      cleanup.Cleaner
	cleanupSet bool // an explicit cleanup URL was configured
	srv        *models.Server
}

// New builds an engine from cfg. onState (may be nil) is called with true when a
// recording starts and false when it stops — an extra hook for UIs beyond the
// built-in floating overlay.
func New(cfg Config, onState func(recording bool)) *Engine {
	var asr transcribe.Transcriber = transcribe.NewStub()
	if cfg.GroqAPIKey != "" {
		asr = transcribe.NewGroq(cfg.GroqAPIKey)
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
	return &Engine{
		rec:        audio.Default(),
		asr:        asr,
		inj:        inject.Default(),
		trig:       hotkey.New(b),
		onState:    onState,
		events:     make(chan bool, 16),
		jobs:       make(chan []byte, 8),
		clean:      clean,
		cleanupSet: cfg.CleanupURL != "",
	}
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

// maybeStartLocalServer spins up the bundled llama-server for cleanup when the
// Qwen model is downloaded and no explicit cleanup URL was configured.
func (e *Engine) maybeStartLocalServer() {
	if e.cleanupSet || !models.Qwen().Installed() {
		return
	}
	go func() {
		srv, url, err := models.StartLlama()
		if err != nil {
			log.Printf("local cleanup server: %v", err)
			return
		}
		e.cleanMu.Lock()
		e.srv = srv
		e.cleanMu.Unlock()
		e.SetCleanupURL(url)
		log.Printf("local cleanup ready at %s", url)
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

func (e *Engine) run() {
	for press := range e.events {
		if press {
			overlay.Show(true)
			e.onState(true)
			if err := e.rec.Start(); err != nil {
				log.Printf("record start: %v", err)
			}
			continue
		}
		overlay.Show(false)
		e.onState(false)
		wav, err := e.rec.Stop()
		if err != nil {
			log.Printf("record stop: %v", err)
			continue
		}
		if len(wav) < minRecordingBytes {
			continue // too short to be speech — ignore the tap
		}
		e.jobs <- wav
	}
}

// processLoop is the single ordered output worker: transcribe -> clean -> inject,
// one recording at a time, so overlapping dictations never corrupt the clipboard.
func (e *Engine) processLoop() {
	for wav := range e.jobs {
		e.process(wav)
	}
}

func (e *Engine) process(wav []byte) {
	raw, err := e.asr.Transcribe(wav)
	if err != nil {
		log.Printf("transcribe: %v", err)
		return
	}
	text, err := e.cleaner().Clean(raw)
	if err != nil {
		log.Printf("cleanup: %v", err)
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
