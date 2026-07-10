// Command 0type is a push-to-talk dictation tool: hold a button, speak, and the
// transcribed + cleaned text is pasted into whatever app has focus.
//
// This is the MVP skeleton. Every stage is an interface (see internal/*), wired
// together here. The default implementations are stubs, so the pipeline runs
// end-to-end with no native dependencies yet — press Enter to simulate a
// dictation. Swap the stubs for the real robotgo / sherpa-onnx / Ollama impls
// one at a time, starting from inject and working backwards.
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

// onPress starts capturing the microphone.
func (p *pipeline) onPress() {
	if err := p.rec.Start(); err != nil {
		log.Printf("record start: %v", err)
	}
}

// onRelease stops capture and runs the recording through the pipeline:
// transcribe -> clean -> inject.
func (p *pipeline) onRelease() {
	pcm, err := p.rec.Stop()
	if err != nil {
		log.Printf("record stop: %v", err)
		return
	}
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
		asr:   transcribe.NewStub(),
		clean: cleanup.NewNoop(),
		inj:   inject.NewStub(),
	}

	trigger := hotkey.NewStdinStub() // TODO: replace with the robotgo global hook
	fmt.Println("0type (skeleton) — press Enter to simulate a dictation, Ctrl+C to quit.")
	if err := trigger.Start(p.onPress, p.onRelease); err != nil {
		log.Fatal(err)
	}
}
