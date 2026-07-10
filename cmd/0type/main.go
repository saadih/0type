// Command 0type is a push-to-talk dictation tool: hold a button, speak, and the
// transcribed + cleaned text is pasted into whatever app has focus.
//
// Console MVP. Trigger, audio capture, transcription (Groq when GROQ_API_KEY is
// set), cleanup (a local LLM when ZEROTYPE_CLEANUP_URL is set), and injection
// are all real.
package main

import (
	"fmt"
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

type pipeline struct {
	rec    audio.Recorder
	asr    transcribe.Transcriber
	clean  cleanup.Cleaner
	inj    inject.Injector
	events chan bool   // hook thread -> run(): true = press, false = release
	jobs   chan []byte // run() -> processLoop(): one finished recording
}

// onPress/onRelease run on the WH_MOUSE_LL callback thread, so they must never
// block — a low-level hook that stalls freezes the whole system's mouse and can
// be dropped by Windows. signal drops the event rather than block.
func (p *pipeline) onPress()   { p.signal(true) }
func (p *pipeline) onRelease() { p.signal(false) }

func (p *pipeline) signal(press bool) {
	select {
	case p.events <- press:
	default:
		log.Printf("hotkey: event queue full, dropped a %s", pressLabel(press))
	}
}

func pressLabel(press bool) string {
	if press {
		return "press"
	}
	return "release"
}

// run serializes recording start/stop off the hook thread and forwards each
// finished recording to the single ordered output worker.
func (p *pipeline) run() {
	for press := range p.events {
		if press {
			fmt.Println("[listening] hold to speak, release to dictate...")
			if err := p.rec.Start(); err != nil {
				log.Printf("record start: %v", err)
			}
			continue
		}
		wav, err := p.rec.Stop()
		if err != nil {
			log.Printf("record stop: %v", err)
			continue
		}
		if len(wav) < minRecordingBytes {
			continue // too short to be speech — ignore the tap
		}
		p.jobs <- wav
	}
}

// processLoop is the single output worker: it transcribes, cleans, and injects
// one recording at a time, in submission order. Serializing injection here is
// what stops overlapping dictations from corrupting the shared clipboard.
func (p *pipeline) processLoop() {
	for wav := range p.jobs {
		p.process(wav)
	}
}

func (p *pipeline) process(wav []byte) {
	raw, err := p.asr.Transcribe(wav)
	if err != nil {
		log.Printf("transcribe: %v", err)
		return
	}
	text, err := p.clean.Clean(raw)
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
	if err := p.inj.Inject(text); err != nil {
		log.Printf("inject: %v", err)
	}
}

func main() {
	p := &pipeline{
		rec:    audio.Default(),
		asr:    transcribe.Default(),
		clean:  cleanup.Default(),
		inj:    inject.Default(),
		events: make(chan bool, 16),
		jobs:   make(chan []byte, 8),
	}
	go p.run()
	go p.processLoop()
	go p.clean.Clean("warm up") // prime the cleanup model's prompt cache in the background

	trigger := hotkey.Default()
	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(set GROQ_API_KEY for real transcription; without it a stub transcript is used)")
	if err := trigger.Start(p.onPress, p.onRelease); err != nil {
		log.Fatal(err)
	}
}
