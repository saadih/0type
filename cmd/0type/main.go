// Command 0type is the console build of the push-to-talk dictation tool: hold
// the mouse back button, speak, release, and the transcribed + cleaned text is
// pasted into whatever app has focus. The GUI build (repo root) shares the same
// engine (internal/app) and adds a settings window.
//
// Environment:
//
//	GROQ_API_KEY          transcribe with Groq's hosted Whisper instead of local Parakeet
//	ZEROTYPE_CLEANUP_URL  clean with an existing OpenAI-compatible server instead of the bundled one
//	OPENROUTER_API_KEY    clean with a hosted model via OpenRouter (OPENROUTER_MODEL picks which)
//	ZEROTYPE_LIVE=0       paste only on release instead of as you speak
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/saadih/0type/internal/app"
	"github.com/saadih/0type/internal/hotkey"
)

func main() {
	cfg := app.Config{
		GroqAPIKey:    os.Getenv("GROQ_API_KEY"),
		CleanupURL:    os.Getenv("ZEROTYPE_CLEANUP_URL"),
		CleanupAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		CleanupModel:  os.Getenv("OPENROUTER_MODEL"),
		Live:          os.Getenv("ZEROTYPE_LIVE") != "0",
		Binding:       hotkey.DefaultBinding(),
		AllowStub:     true, // console/dev: canned transcript when no key/model
	}
	if cfg.GroqAPIKey != "" {
		cfg.Transcription = "groq"
	}
	if cfg.CleanupAPIKey != "" {
		cfg.Cleanup = "openrouter"
	}
	engine := app.New(cfg, func(recording bool) {
		if recording {
			fmt.Println("[listening] hold to speak, release to dictate...")
		}
	})

	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(set GROQ_API_KEY for cloud transcription; without it and without Parakeet, a stub transcript is used)")
	if err := engine.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "hotkey:", err)
		os.Exit(1)
	}

	// Block until Ctrl+C; the engine runs on its own goroutines.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
