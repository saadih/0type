package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/saadih/0type/internal/app"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Settings is the user-facing configuration edited in the window and persisted
// to the OS config dir (%APPDATA%\0type\config.json on Windows).
type Settings struct {
	Trigger    string `json:"trigger"`    // e.g. "MouseBack"
	Mode       string `json:"mode"`       // "hold" | "toggle"
	Language   string `json:"language"`   // "auto" | "en" | "sv"
	GroqAPIKey string `json:"groqApiKey"` // cloud transcription (optional)
	CleanupURL string `json:"cleanupUrl"` // local LLM base URL (optional)
}

func defaultSettings() Settings {
	return Settings{Trigger: "MouseBack", Mode: "hold", Language: "auto"}
}

// App is the Wails backend bound to the frontend.
type App struct {
	ctx      context.Context
	mu       sync.Mutex
	settings Settings
	path     string
	engine   *app.Engine
}

// NewApp creates the app with defaults and the config-file path resolved.
func NewApp() *App {
	return &App{settings: defaultSettings(), path: configPath()}
}

// startup runs when the window is ready: load settings, then start the engine.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.load()
	a.startEngine()
}

// startEngine builds the dictation engine from the saved settings and starts it.
// The onState callback drives the window's recording indicator over a Wails event.
func (a *App) startEngine() {
	s := a.GetSettings()
	a.engine = app.New(app.Config{
		GroqAPIKey: s.GroqAPIKey,
		CleanupURL: s.CleanupURL,
	}, func(recording bool) {
		runtime.EventsEmit(a.ctx, "recording", recording)
	})
	a.engine.Start()
}

// GetSettings returns the current settings (the frontend calls this on load).
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// SaveSettings persists the settings edited in the window. Transcription/cleanup
// changes apply on the next launch.
func (a *App) SaveSettings(s Settings) error {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return a.save()
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "0type", "config.json")
}

func (a *App) load() {
	b, err := os.ReadFile(a.path)
	if err != nil {
		return // no config yet — keep defaults
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err == nil {
		a.mu.Lock()
		a.settings = s
		a.mu.Unlock()
	}
}

func (a *App) save() error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, b, 0o644)
}
