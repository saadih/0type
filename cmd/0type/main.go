// Command 0type is the console build of the push-to-talk dictation tool: hold
// the mouse back button, speak, release, and the transcribed + cleaned text is
// pasted into whatever app has focus. The GUI build (repo root) shares the same
// engine (internal/app) and adds a settings window.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/saadih/0type/internal/app"
	"github.com/saadih/0type/internal/hotkey"
)

func main() {
	engine := app.New(app.Config{
		GroqAPIKey: os.Getenv("GROQ_API_KEY"),
		CleanupURL: os.Getenv("ZEROTYPE_CLEANUP_URL"),
		Binding:    hotkey.DefaultBinding(),
		AllowStub:  true, // console/dev: canned transcript when no key/model
	}, func(recording bool) {
		if recording {
			fmt.Println("[listening] hold to speak, release to dictate...")
		}
	})

	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(set GROQ_API_KEY for real transcription; without it a stub transcript is used)")
	if err := engine.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "hotkey:", err)
		os.Exit(1)
	}

	// Block until Ctrl+C; the engine runs on its own goroutines.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
