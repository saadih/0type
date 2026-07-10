// Command 0type is a push-to-talk dictation tool: hold a button, speak, and the
// transcribed + cleaned text is pasted into whatever app has focus.
//
// This is the console MVP. The trigger is real (hold the mouse back button on
// Windows — see internal/hotkey); the record/transcribe/cleanup/inject stages
// are still stubs, swapped in one at a time behind their interfaces.
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

// pipeline holds the four stages of a dictation.
type pipeline struct {
	rec   audio.Recorder
	asr   transcribe.Transcriber
	clean cleanup.Cleaner
	inj   inject.Injector
}

// onPress starts capturing the microphone. It runs on the hook thread, so it
// must stay fast.
func (p *pipeline) onPress() {
	fmt.Println("[listening] hold to speak, release to dictate...")
	if err := p.rec.Start(); err != nil {
		log.Printf("record start: %v", err)
	}
}

// onRelease stops capture and hands the audio off to a worker goroutine. The
// heavy work (transcribe/clean/inject) must not run on the hook thread — a slow
// low-level hook callback lags the whole system's mouse.
func (p *pipeline) onRelease() {
	pcm, err := p.rec.Stop()
	if err != nil {
		log.Printf("record stop: %v", err)
		return
	}
	go p.process(pcm)
}

// process runs the recorded audio through transcribe -> clean -> inject.
func (p *pipeline) process(pcm []byte) {
	raw, err := p.asr.Transcribe(pcm)
	if err != nil {
		log.Printf("transcribe: %v", err)
		return
	}
	text, err := p.clean.Clean(raw)
	if err != nil {
		log.Printf("cleanup: %v", err)
		text = raw // fall back to the raw transcript rather than dropping it
	}
	if err := p.inj.Inject(text); err != nil {
		log.Printf("inject: %v", err)
	}
}

func main() {
	p := &pipeline{
		rec:   audio.NewStub(),
		asr:   transcribe.Default(),
		clean: cleanup.NewNoop(),
		inj:   inject.Default(),
	}

	trigger := hotkey.Default()
	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(console MVP - audio + transcription still stubbed; the stub transcript is pasted at your cursor)")
	if err := trigger.Start(p.onPress, p.onRelease); err != nil {
		log.Fatal(err)
	}
}
