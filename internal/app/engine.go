// Package app is the shared dictation engine used by both the console
// (cmd/0type) and the Wails GUI: hold a trigger, record the mic, transcribe,
// clean, and paste, one ordered output at a time. In live mode the recording
// is split at pauses while the speaker is still talking, so text lands piece
// by piece instead of all at once on release.
package app

import (
	"errors"
	"fmt"
	"log"
	"strings"
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

// minSpeechBytes ignores fat-finger taps: below ~0.15s of 16 kHz mono 16-bit
// audio there is no speech, only a wasted transcription round-trip.
const minSpeechBytes = audio.BytesPerSecond * 15 / 100

// Config selects the backends and the initial trigger binding.
type Config struct {
	// OpenRouterAPIKey unlocks the hosted backends below; both stages share it.
	OpenRouterAPIKey string

	// Transcription picks the backend: "local" (Parakeet, the default) or
	// "openrouter" (a hosted speech-to-text model; TranscriptionModel is
	// optional). Without a usable choice the engine falls back to Parakeet if
	// downloaded, else the stub (AllowStub) or a placeholder that asks for a
	// model download.
	Transcription      string
	TranscriptionModel string

	// Cleanup picks the backend: "local" (the bundled Qwen server, the default)
	// or "openrouter" (a hosted chat model; CleanupModel is optional).
	// CleanupURL points local cleanup at an existing OpenAI-compatible server
	// instead of the bundled one.
	Cleanup      string
	CleanupModel string
	CleanupURL   string
	// Notes is the user's note to the cleanup model: names and jargon to spell
	// right, preferences to follow.
	Notes string

	// Live pastes as you speak: the recording is cut at pauses and each piece
	// is transcribed, cleaned, and pasted while the mic stays open.
	Live bool

	Binding     hotkey.Binding
	Mode        string // "hold" (default) | "toggle"
	InputDevice string // microphone name; empty -> system default
	AllowStub   bool   // console/dev: fall back to the canned stub transcript when nothing else is available
	// Notify (may be nil) surfaces user-facing messages to a UI. kind is
	// "info" or "error".
	Notify func(kind, msg string)
}

// job is one piece of audio for the output worker: a whole recording, or one
// pause-delimited segment of a live one. id ties segments of the same
// dictation together so cleanup can see what was pasted just before.
type job struct {
	wav []byte
	id  uint64
}

// Engine wires the dictation pipeline together and runs it.
type Engine struct {
	rec     audio.Recorder
	streams bool // rec delivers audio while recording (audio.Streaming)
	inj     inject.Injector
	trig    hotkey.Controller
	onState func(recording bool)
	events  chan bool
	jobs    chan job

	asrMu       sync.Mutex             // guards the transcriber fields
	asr         transcribe.Transcriber // active backend
	baseASR     transcribe.Transcriber // stub or "download a model" placeholder
	localASR    transcribe.Transcriber // Parakeet, once loaded
	cloudASR    transcribe.Transcriber // OpenRouter, when a key is set
	useCloudASR bool

	cleanMu       sync.Mutex // guards the cleaner fields and srv
	clean         cleanup.Cleaner
	localClean    *cleanup.LLM // the local server, once reachable
	localURL      string       // local endpoint; "" until the bundled server is up
	cloudClean    *cleanup.LLM // hosted endpoint, when a key is set
	cloudKey      string
	cloudModel    string
	useCloudClean bool
	notes         string // the user's note to the cleanup model
	srv           *models.Server
	srvMu         sync.Mutex // serializes local cleanup-server startup

	toggleMode atomic.Bool // true -> tap to toggle; false -> hold to talk
	live       atomic.Bool // paste as you speak (setting)
	liveRec    atomic.Bool // the current recording is being segmented
	seg        *audio.Segmenter
	recBuf     []byte                 // streamed audio of a non-live recording (recorder goroutine, then stopCapture)
	behind     atomic.Bool            // a segment was dropped because the output worker is behind
	dictID     atomic.Uint64          // increments per recording
	notify     func(kind, msg string) // optional UI notifier; set once before Start

	ovMu       sync.Mutex // guards the overlay state below
	recording  bool
	processing int // in-flight jobs being transcribed/cleaned
}

// New builds an engine from cfg. onState (may be nil) is called with true when a
// recording starts and false when it stops, an extra hook for UIs beyond the
// built-in floating overlay.
func New(cfg Config, onState func(recording bool)) *Engine {
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
		rec:      rec,
		inj:      inject.Default(),
		trig:     hotkey.New(b),
		onState:  onState,
		events:   make(chan bool, 16),
		jobs:     make(chan job, 256),
		seg:      audio.NewSegmenter(),
		notify:   cfg.Notify,
		localURL: cfg.CleanupURL,
		notes:    cfg.Notes,
	}
	e.toggleMode.Store(cfg.Mode == "toggle")
	e.live.Store(cfg.Live)
	if st, ok := rec.(audio.Streaming); ok {
		st.OnAudio(e.onAudio)
		e.streams = true
	}

	// Transcription: the placeholder is the canned stub for console/dev, else
	// one that reports ErrNoModel so the GUI prompts for a download instead of
	// pasting fake text. Local Parakeet loads when downloaded and compiled in
	// (built with -tags parakeet; NewParakeet errors on the stub build).
	if cfg.AllowStub {
		e.baseASR = transcribe.NewStub()
	} else {
		e.baseASR = transcribe.NewNeedModel()
	}
	if models.Parakeet().Installed() {
		if dir, err := models.ExtractParakeet(); err == nil {
			if p, err := transcribe.NewParakeet(dir); err == nil {
				e.localASR = p
			}
		}
	}
	if cfg.OpenRouterAPIKey != "" {
		e.cloudASR = transcribe.NewOpenRouter(cfg.OpenRouterAPIKey, cfg.TranscriptionModel)
	}
	e.useCloudASR = cfg.Transcription == "openrouter" && e.cloudASR != nil
	e.refreshASR()

	// Cleanup: an explicit local URL is used as-is; the bundled server fills
	// localURL in later (see maybeStartLocalServer).
	if cfg.CleanupURL != "" {
		e.localClean = e.newLocalCleaner(cfg.CleanupURL)
	}
	e.cloudKey, e.cloudModel = cfg.OpenRouterAPIKey, cfg.CleanupModel
	e.cloudClean = e.newCloudCleaner()
	e.useCloudClean = cfg.Cleanup == "openrouter" && e.cloudClean != nil
	e.refreshClean()
	return e
}

// newLocalCleaner builds the local cleaner with the current notes. The caller
// holds cleanMu (or is still constructing the engine).
func (e *Engine) newLocalCleaner(url string) *cleanup.LLM {
	c := cleanup.NewLLM(url)
	c.Notes = e.notes
	return c
}

// newCloudCleaner builds the hosted cleaner from the stored key and model, or
// nil without a key. The caller holds cleanMu (or is still constructing).
func (e *Engine) newCloudCleaner() *cleanup.LLM {
	if e.cloudKey == "" {
		return nil
	}
	c := cleanup.NewCloud(cleanup.OpenRouterURL, e.cloudKey, e.cloudModel)
	c.Notes = e.notes
	return c
}

// SetNotes updates the user's note to the cleanup model live. Cleaners are
// rebuilt rather than mutated, since the output worker may be mid-request.
func (e *Engine) SetNotes(notes string) {
	e.cleanMu.Lock()
	defer e.cleanMu.Unlock()
	if notes == e.notes {
		return
	}
	e.notes = notes
	if e.localURL != "" {
		e.localClean = e.newLocalCleaner(e.localURL)
	}
	e.cloudClean = e.newCloudCleaner()
	e.refreshClean()
}

// Start launches the engine's goroutines, the floating overlay, the bundled
// cleanup server (if applicable), and the global trigger.
func (e *Engine) Start() error {
	overlay.Start()
	e.maybeStartLocalServer()
	go e.run()
	go e.processLoop()
	e.warmLocalCleanup()
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

// refreshASR picks the active transcriber from what is configured and loaded:
// the cloud backend when chosen, else local Parakeet, else the placeholder.
// The caller holds asrMu.
func (e *Engine) refreshASR() {
	switch {
	case e.useCloudASR && e.cloudASR != nil:
		e.asr = e.cloudASR
	case e.localASR != nil:
		e.asr = e.localASR
	default:
		e.asr = e.baseASR
	}
}

// refreshClean picks the active cleaner: the hosted backend when chosen, else
// the local server once it is reachable, else pass-through. The caller holds
// cleanMu.
func (e *Engine) refreshClean() {
	switch {
	case e.useCloudClean && e.cloudClean != nil:
		e.clean = e.cloudClean
	case e.localClean != nil:
		e.clean = e.localClean
	default:
		e.clean = cleanup.NewNoop()
	}
}

// SetTranscription switches between local Parakeet ("local") and a hosted
// speech-to-text model via OpenRouter ("openrouter", with the API key and an
// optional model) live. An empty key falls back to local.
func (e *Engine) SetTranscription(kind, apiKey, model string) {
	e.asrMu.Lock()
	defer e.asrMu.Unlock()
	if apiKey != "" {
		e.cloudASR = transcribe.NewOpenRouter(apiKey, model)
	} else {
		e.cloudASR = nil
	}
	e.useCloudASR = kind == "openrouter" && e.cloudASR != nil
	e.refreshASR()
}

// SetCleanup switches between the bundled local model ("local") and a hosted
// model via OpenRouter ("openrouter", with its API key and optional model)
// live. An empty key falls back to local, starting the bundled server if the
// model is downloaded and it isn't running yet.
func (e *Engine) SetCleanup(kind, apiKey, model string) {
	e.cleanMu.Lock()
	e.cloudKey, e.cloudModel = apiKey, model
	e.cloudClean = e.newCloudCleaner()
	e.useCloudClean = kind == "openrouter" && e.cloudClean != nil
	e.refreshClean()
	cloud := e.useCloudClean
	e.cleanMu.Unlock()
	if !cloud {
		e.maybeStartLocalServer()
	}
}

// SetLive turns paste-as-you-speak on or off. It applies to the next recording.
func (e *Engine) SetLive(on bool) { e.live.Store(on) }

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
	e.localASR = p
	e.refreshASR()
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

// setLocalCleanupURL points local cleanup at url, makes it active unless a
// hosted backend is chosen, and pre-warms it.
func (e *Engine) setLocalCleanupURL(url string) {
	e.cleanMu.Lock()
	e.localURL = url
	if url == "" {
		e.localClean = nil
	} else {
		e.localClean = e.newLocalCleaner(url)
	}
	e.refreshClean()
	e.cleanMu.Unlock()
	e.warmLocalCleanup()
}

// warmLocalCleanup fires a throwaway request at the local server so the first
// real dictation reuses the cached system prompt. Hosted backends are never
// warmed; that would just cost money.
func (e *Engine) warmLocalCleanup() {
	e.cleanMu.Lock()
	c := e.localClean
	e.cleanMu.Unlock()
	if c != nil {
		go func() { _, _ = c.Clean("warm up", "") }()
	}
}

// EnableLocalCleanup starts the bundled llama-server for the downloaded Qwen
// model and points local cleanup at it, so a fresh download takes effect
// without a restart. It is a no-op when an explicit cleanup URL was configured
// or a local server is already running, and it stores the server so Stop can
// shut it down.
func (e *Engine) EnableLocalCleanup() error {
	e.srvMu.Lock()
	defer e.srvMu.Unlock()
	e.cleanMu.Lock()
	skip := e.localURL != "" || e.srv != nil
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
	e.setLocalCleanupURL(url)
	return nil
}

// maybeStartLocalServer brings up local cleanup in the background when the
// Qwen model is downloaded and no local endpoint is known yet.
func (e *Engine) maybeStartLocalServer() {
	e.cleanMu.Lock()
	have := e.localURL != "" || e.srv != nil
	e.cleanMu.Unlock()
	if have || !models.Qwen().Installed() {
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
	// The recorder's stream goroutine does not exist yet, so the segmenter can
	// be reset without a lock. Live mode is latched per recording so a
	// settings change mid-dictation can't desynchronize the segment offsets.
	e.dictID.Add(1)
	e.seg.Reset()
	e.recBuf = e.recBuf[:0]
	e.liveRec.Store(e.live.Load() && e.streams)
	if err := e.rec.Start(); err != nil {
		log.Printf("record start: %v", err)
		e.emit("error", "Could not open the microphone.")
		e.onState(false)
		e.setRecording(false)
		return false
	}
	return true
}

// onAudio runs on the recorder's goroutine with each captured chunk. In live
// mode it feeds the segmenter, which hands finished utterances to the output
// worker while the mic is still open; otherwise it just collects the
// recording for stopCapture.
func (e *Engine) onAudio(pcm []byte) {
	if !e.liveRec.Load() {
		e.recBuf = append(e.recBuf, pcm...)
		return
	}
	id := e.dictID.Load()
	e.seg.Push(pcm, func(seg []byte) {
		e.enqueue(job{wav: audio.EncodeWAV(seg), id: id})
	})
}

// enqueue marks a job in flight (blue dot) and hands it to the output worker.
// It never blocks: the recorder's goroutine calls it, and stalling that starves
// the capture ring. If the worker is that far behind (a hung backend), the
// segment is dropped and the user told once.
func (e *Engine) enqueue(j job) {
	select {
	case e.jobs <- j:
		e.addProcessing(1)
	default:
		log.Printf("output queue full; dropped a %d-byte segment", len(j.wav))
		if !e.behind.Swap(true) {
			e.emit("error", "Transcription is falling behind; some speech was dropped.")
		}
	}
}

// stopCapture ends the recording and hands what is left to the output worker:
// the whole recording, or in live mode only the tail after the last segment.
func (e *Engine) stopCapture() {
	e.onState(false)
	wav, err := e.rec.Stop()
	if err != nil {
		e.setRecording(false)
		log.Printf("record stop: %v", err)
		return
	}
	// Stop flushed every chunk through onAudio, so a streaming recorder's
	// audio is in the segmenter (live) or recBuf (not live); a non-streaming
	// one returned it in the WAV.
	var pcm []byte
	var skip bool
	switch {
	case e.liveRec.Load():
		pcm = e.seg.Tail()
		// A tail without detected speech is normally the pause after the last
		// segment. If nothing was ever cut, though, the gate may simply have
		// missed a quiet speaker, so let the transcriber judge the whole take.
		skip = e.seg.Offset() > 0 && !e.seg.TailHasSpeech()
	case e.streams:
		pcm = e.recBuf
	default:
		if len(wav) >= audio.HeaderBytes {
			pcm = wav[audio.HeaderBytes:]
		}
	}
	if skip || len(pcm) < minSpeechBytes { // too short to be speech: ignore the tap
		e.setRecording(false)
		return
	}
	wav = audio.EncodeWAV(pcm)
	// Mark processing before clearing recording so the dot goes red -> blue with
	// no hidden flicker; the output worker clears it when done.
	e.addProcessing(1)
	e.setRecording(false)
	e.jobs <- job{wav: wav, id: e.dictID.Load()}
	e.behind.Store(false)
}

// processLoop is the single ordered output worker: transcribe -> clean -> inject,
// one piece of audio at a time, so overlapping dictations never corrupt the
// clipboard. Within one dictation it remembers what was pasted last, so the
// next segment is cleaned with that context and joined with a space.
func (e *Engine) processLoop() {
	var lastID uint64
	var lastText string
	for j := range e.jobs {
		prev := ""
		if j.id == lastID {
			prev = lastText
		}
		if text := e.process(j.wav, prev); text != "" {
			lastID, lastText = j.id, text
		}
	}
}

// process runs one piece of audio through the pipeline and returns what it
// pasted ("" if nothing).
func (e *Engine) process(wav []byte, prev string) string {
	defer e.addProcessing(-1) // clear the blue dot when this job finishes
	raw, err := e.transcriber().Transcribe(wav)
	if err != nil {
		if errors.Is(err, transcribe.ErrNoModel) {
			e.emit("error", "Download a transcription model to start dictating.")
		} else {
			log.Printf("transcribe: %v", err)
			e.emit("error", "Transcription failed.")
		}
		return ""
	}
	if strings.TrimSpace(raw) == "" {
		return "" // silence
	}
	text, err := e.cleaner().Clean(raw, prev)
	if err != nil {
		log.Printf("cleanup: %v", err)
		e.emit("info", "Cleanup unavailable; pasted the raw transcript.")
		text = raw // fall back to the raw transcript rather than dropping it
	}
	if text == "" {
		log.Printf("cleanup returned empty; nothing pasted (if unexpected, start the cleanup server with --jinja)")
		return "" // nothing to paste (a filler-only utterance)
	}
	paste := text
	if prev != "" && !strings.HasSuffix(prev, "\n") && !strings.HasPrefix(text, "\n") {
		paste = " " + text // continue the dictation already on screen
	}
	if err := e.inj.Inject(paste); err != nil {
		log.Printf("inject: %v", err)
	}
	return text
}
