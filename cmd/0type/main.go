// Command 0type is the console build of the push-to-talk dictation tool: hold
// the mouse back button, speak, release, and the transcribed + cleaned text is
// pasted into whatever app has focus. The GUI build (repo root) shares the same
// engine (internal/app) and adds a settings window.
//
// Environment:
//
//	OPENROUTER_API_KEY    run transcription and cleanup through OpenRouter instead of the local models
//	OPENROUTER_STT_MODEL  transcription model on OpenRouter (default openai/whisper-large-v3-turbo)
//	OPENROUTER_MODEL      cleanup model on OpenRouter (default anthropic/claude-haiku-4.5)
//	ZEROTYPE_CLEANUP_URL  clean with an existing OpenAI-compatible server instead of the bundled one
//	ZEROTYPE_NOTES        a note to the cleanup model (names, jargon, preferences)
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
		OpenRouterAPIKey:   os.Getenv("OPENROUTER_API_KEY"),
		TranscriptionModel: os.Getenv("OPENROUTER_STT_MODEL"),
		CleanupModel:       os.Getenv("OPENROUTER_MODEL"),
		CleanupURL:         os.Getenv("ZEROTYPE_CLEANUP_URL"),
		Notes:              os.Getenv("ZEROTYPE_NOTES"),
		Live:               os.Getenv("ZEROTYPE_LIVE") != "0",
		Binding:            hotkey.DefaultBinding(),
		AllowStub:          true, // console/dev: canned transcript when no key/model
	}
	if cfg.OpenRouterAPIKey != "" {
		cfg.Transcription = "openrouter"
		if cfg.CleanupURL == "" { // an explicit local server wins over the cloud
			cfg.Cleanup = "openrouter"
		}
	}
	engine := app.New(cfg, func(recording bool) {
		if recording {
			fmt.Println("[listening] hold to speak, release to dictate...")
		}
	})

	fmt.Println("0type - focus a text field, hold the mouse back button (MB4), speak, release. Ctrl+C to quit.")
	fmt.Println("(set OPENROUTER_API_KEY for cloud transcription; without it and without Parakeet, a stub transcript is used)")
	if err := engine.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "hotkey:", err)
		os.Exit(1)
	}

	// Block until Ctrl+C; the engine runs on its own goroutines.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
