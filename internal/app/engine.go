// Package app is the shared dictation engine used by both the console
// (cmd/0type) and the Wails GUI: hold a trigger, record the mic, transcribe,
// clean, and paste — one ordered output at a time.
package app

import (
	"log"

	"github.com/saadih/0type/internal/audio"
	"github.com/saadih/0type/internal/cleanup"
	"github.com/saadih/0type/internal/hotkey"
	"github.com/saadih/0type/internal/inject"
	"github.com/saadih/0type/internal/transcribe"
)

// minRecordingBytes ignores fat-finger taps: below ~0.15s of 16 kHz mono 16-bit
// audio there is no speech, only a wasted transcription round-trip.
const minRecordingBytes = 44 + 16000*2*15/100

// Config selects the transcription and cleanup backends.
type Config struct {
	GroqAPIKey string // cloud transcription; empty -> stub transcript
	CleanupURL string // local LLM base URL; empty -> pass-through
}

// Engine wires the dictation pipeline together and runs it.
type Engine struct {
	rec     audio.Recorder
	asr     transcribe.Transcriber
	clean   cleanup.Cleaner
	inj     inject.Injector
	trig    hotkey.Trigger
	onState func(recording bool)
	events  chan bool
	jobs    chan []byte
}

// New builds an engine from cfg. onState (may be nil) is called with true when a
// recording starts and false when it stops — used for the UI recording indicator.
func New(cfg Config, onState func(recording bool)) *Engine {
	var asr transcribe.Transcriber = transcribe.NewStub()
	if cfg.GroqAPIKey != "" {
		asr = transcribe.NewGroq(cfg.GroqAPIKey)
	}
	var clean cleanup.Cleaner = cleanup.NewNoop()
	if cfg.CleanupURL != "" {
		clean = cleanup.NewLLM(cfg.CleanupURL)
	}
	if onState == nil {
		onState = func(bool) {}
	}
	return &Engine{
		rec:     audio.Default(),
		asr:     asr,
		clean:   clean,
		inj:     inject.Default(),
		trig:    hotkey.Default(),
		onState: onState,
		events:  make(chan bool, 16),
		jobs:    make(chan []byte, 8),
	}
}

// Start launches the engine's goroutines and installs the global trigger. It
// returns immediately; the trigger runs on its own goroutine.
func (e *Engine) Start() {
	go e.run()
	go e.processLoop()
	go func() { _, _ = e.clean.Clean("warm up") }() // prime the cleanup prompt cache
	go func() {
		if err := e.trig.Start(e.onPress, e.onRelease); err != nil {
			log.Printf("hotkey: %v", err)
		}
	}()
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
			e.onState(true)
			if err := e.rec.Start(); err != nil {
				log.Printf("record start: %v", err)
			}
			continue
		}
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
	text, err := e.clean.Clean(raw)
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
