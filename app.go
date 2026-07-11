package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/saadih/0type/internal/app"
	"github.com/saadih/0type/internal/hotkey"
	"github.com/saadih/0type/internal/models"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Settings is the user-facing configuration edited in the window and persisted
// to the OS config dir (%APPDATA%\0type\config.json on Windows).
type Settings struct {
	Trigger  hotkey.Binding `json:"trigger"`
	Mode     string         `json:"mode"`     // "hold" | "toggle"
	Language string         `json:"language"` // "auto" | "en" | "sv"
}

func defaultSettings() Settings {
	return Settings{Trigger: hotkey.DefaultBinding(), Mode: "hold", Language: "auto"}
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

// shutdown stops the bundled cleanup server when the window closes.
func (a *App) shutdown(ctx context.Context) {
	if a.engine != nil {
		a.engine.Stop()
	}
}

// startEngine builds the dictation engine from the saved settings and starts it.
func (a *App) startEngine() {
	s := a.GetSettings()
	a.engine = app.New(app.Config{Binding: s.Trigger}, nil)
	_ = a.engine.Start()
}

// GetSettings returns the current settings (the frontend calls this on load).
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// SaveSettings persists the settings edited in the window. The trigger applies
// live via CaptureBinding; model downloads apply live via DownloadParakeet and
// DownloadQwen.
func (a *App) SaveSettings(s Settings) error {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return a.save()
}

// CaptureBinding waits for the user to press any key or mouse side/middle button,
// applies it as the trigger live, persists it, and returns the new binding.
func (a *App) CaptureBinding() (hotkey.Binding, error) {
	if a.engine == nil {
		return hotkey.Binding{}, nil
	}
	b, err := a.engine.Rebind()
	if err != nil {
		return b, err
	}
	a.mu.Lock()
	a.settings.Trigger = b
	a.mu.Unlock()
	_ = a.save()
	return b, nil
}

// ModelState reports which downloadable models are installed.
func (a *App) ModelState() map[string]bool {
	return map[string]bool{
		"qwen":     models.Qwen().Installed(),
		"parakeet": models.Parakeet().Installed(),
	}
}

// DownloadQwen fetches the cleanup model + the llama-server binary (progress is
// emitted as "download-progress" events), then starts local cleanup and emits
// "model-ready". Runs on a Wails goroutine, so it may block on the ~2.7GB fetch.
func (a *App) DownloadQwen() error {
	server, err := models.LlamaServer()
	if err != nil {
		return err
	}
	if !server.Installed() {
		if err := models.Download(server, a.progress("qwen")); err != nil {
			return err
		}
	}
	gguf := models.Qwen()
	if !gguf.Installed() {
		if err := models.Download(gguf, a.progress("qwen")); err != nil {
			return err
		}
	}
	go func() {
		if a.engine == nil {
			return
		}
		if err := a.engine.EnableLocalCleanup(); err != nil {
			runtime.EventsEmit(a.ctx, "model-error", err.Error())
			return
		}
		runtime.EventsEmit(a.ctx, "model-ready", "qwen")
	}()
	return nil
}

// DownloadParakeet fetches + extracts the local transcription model, then swaps
// it into the running engine so it takes effect without a restart.
func (a *App) DownloadParakeet() error {
	m := models.Parakeet()
	if !m.Installed() {
		if err := models.Download(m, a.progress("parakeet")); err != nil {
			return err
		}
	}
	if _, err := models.ExtractParakeet(); err != nil {
		return err
	}
	if a.engine != nil {
		if err := a.engine.EnableLocalTranscription(); err != nil {
			// Download and extract worked; only the live swap failed (e.g. a
			// build without -tags parakeet). Keep the model, report the miss.
			runtime.EventsEmit(a.ctx, "model-error", err.Error())
			return nil
		}
	}
	runtime.EventsEmit(a.ctx, "model-ready", "parakeet")
	return nil
}

// ParakeetSupported reports whether this build includes local transcription
// (built with -tags parakeet).
func (a *App) ParakeetSupported() bool { return parakeetSupported }

func (a *App) progress(id string) func(done, total int64) {
	return func(done, total int64) {
		runtime.EventsEmit(a.ctx, "download-progress", map[string]any{"id": id, "done": done, "total": total})
	}
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
	a.mu.Lock()
	b, err := json.MarshalIndent(a.settings, "", "  ")
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, b, 0o644)
}
