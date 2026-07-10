// Command 0type is a push-to-talk dictation tool: hold a button, speak, and the
// transcribed + cleaned text is pasted into whatever app has focus.
//
// Console MVP. Trigger, audio capture, transcription (Groq when GROQ_API_KEY is
// set), and injection are all real; cleanup is still a pass-through stub.
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

type pipeline struct {
	rec    audio.Recorder
	asr    transcribe.Transcriber
	clean  cleanup.Cleaner
	inj    inject.Injector
	events chan bool // true = press (start recording), false = release (stop)
}

// onPress/onRelease run on the hook thread, so they only signal the worker —
// opening an audio device or making a network call here would lag the whole
// system's mouse (and Windows may drop a slow low-level hook).
func (p *pipeline) onPress()   { p.events <- true }
func (p *pipeline) onRelease() { p.events <- false }

// run serializes recording start/stop off the hook thread, then hands each
// finished recording to its own transcription goroutine.
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
		go p.process(wav)
	}
}

// process runs a finished recording through transcribe -> clean -> inject.
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
		return // nothing recognized (e.g. silence) — don't paste an empty string
	}
	if err := p.inj.Inject(text); err != nil {
		log.Printf("inject: %v", err)
	}
}

func main() {
	p := &pipeline{
		rec:    audio.Default(),
		asr:    transcribe.Default(),
		clean:  cleanup.NewNoop(),
		inj:    inject.Default(),
		events: make(chan bool, 16),
	}
	go p.run()

	trigger := hotkey.Default()
	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(set GROQ_API_KEY for real transcription; without it a stub transcript is used)")
	if err := trigger.Start(p.onPress, p.onRelease); err != nil {
		log.Fatal(err)
	}
}
