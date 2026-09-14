package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/saadih/0type/internal/app"
	"github.com/saadih/0type/internal/audio"
	"github.com/saadih/0type/internal/autostart"
	"github.com/saadih/0type/internal/cleanup"
	"github.com/saadih/0type/internal/hotkey"
	"github.com/saadih/0type/internal/models"
	"github.com/saadih/0type/internal/recommend"
	"github.com/saadih/0type/internal/tray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Version is the app version shown in the window footer. Bump it per release.
const Version = "0.3.0"

// Settings is the user-facing configuration edited in the window and persisted
// to the OS config dir (%APPDATA%\0type\config.json on Windows). API keys are
// stored there in plain text, readable only by the user's own account.
type Settings struct {
	Trigger     hotkey.Binding `json:"trigger"`
	Mode        string         `json:"mode"`        // "hold" | "toggle"
	InputDevice string         `json:"inputDevice"` // microphone name; "" = system default
	Output      string         `json:"output"`      // "live" (paste as you speak, default) | "end" (paste on release)

	OpenRouterKey string `json:"openrouterApiKey"` // for hosted cleanup; audio is never sent

	Cleaner      string `json:"cleaner"`      // "local" (Qwen, default) | "openrouter"
	CleanupModel string `json:"cleanupModel"` // "" = cleanup.DefaultCloudModel

	// Notes is the user's note to the cleanup model: names and jargon to spell
	// right, preferences to follow.
	Notes string `json:"notes"`

	// The user's own OpenRouter model slugs, listed first in the Recommended
	// picker (settings page).
	MyCleanupModels []string `json:"myCleanupModels"`
}

// maxNotes bounds the About-you note so it always fits the local model's 4k
// context next to the prompt and a transcript chunk (roughly 400 tokens).
const maxNotes = 1500

func defaultSettings() Settings {
	return Settings{Trigger: hotkey.DefaultBinding(), Mode: "hold", Output: "live", Cleaner: "local"}
}

// live reports whether the settings ask for paste-as-you-speak. Configs written
// before the option existed have no "output" field and get the default.
func (s Settings) live() bool { return s.Output != "end" }

// validate rejects a cloud choice without the key it needs, so a save never
// silently falls back to local.
func (s Settings) validate() error {
	if s.Cleaner == "openrouter" && s.OpenRouterKey == "" {
		return fmt.Errorf("enter an OpenRouter API key, or download a local model in Settings")
	}
	if len(s.Notes) > maxNotes {
		return fmt.Errorf("keep About you under %d characters; it rides along with every request", maxNotes)
	}
	return nil
}

// App is the Wails backend bound to the frontend.
type App struct {
	ctx      context.Context
	mu       sync.Mutex
	settings Settings
	path     string
	engine   *app.Engine
	quitting atomic.Bool // true once the user picked tray Quit, so close really closes
}

// NewApp creates the app with defaults and the config-file path resolved.
func NewApp() *App {
	return &App{settings: defaultSettings(), path: configPath()}
}

// trayTip is the tray icon's resting tooltip, shown once 0type is ready to
// dictate. While a model is still loading the engine replaces it.
const trayTip = "0type — no typing allowed"

// startup runs when the window is ready: load settings, install the tray icon,
// and start the engine. The tray goes first so the engine's opening status
// lands on an icon that already exists.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.load()
	tray.Start(trayTip, a.trayOpen, a.trayQuit)
	a.startEngine()
}

// shutdown stops the bundled cleanup server and removes the tray icon.
func (a *App) shutdown(ctx context.Context) {
	tray.Stop()
	if a.engine != nil {
		a.engine.Stop()
	}
}

// beforeClose runs when the window's close button is pressed (or Quit is
// called). Unless the user chose Quit, it hides to the tray and keeps running.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false // let the app actually quit
	}
	runtime.WindowHide(ctx)
	return true // keep running in the tray instead
}

// trayOpen restores the window from the tray.
func (a *App) trayOpen() {
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
}

// trayQuit exits for real (past beforeClose's hide-to-tray).
func (a *App) trayQuit() {
	a.quitting.Store(true)
	runtime.Quit(a.ctx)
}

// startEngine builds the dictation engine from the saved settings and starts it.
func (a *App) startEngine() {
	s := a.GetSettings()
	a.engine = app.New(app.Config{
		OpenRouterAPIKey: s.OpenRouterKey,
		Cleanup:          s.Cleaner,
		CleanupModel:     s.CleanupModel,
		Notes:            s.Notes,
		Live:             s.live(),
		Binding:          s.Trigger,
		Mode:             s.Mode,
		InputDevice:      s.InputDevice,
		Notify:           a.notify,
	}, nil)
	_ = a.engine.Start()
}

// notify forwards engine messages to the frontend as a "notice" event, except
// for startup status, which belongs on the tray icon: it is a standing fact
// about whether dictating will work yet, not a one-off message, and the window
// is usually closed while it is true.
func (a *App) notify(kind, msg string) {
	if kind == "status" {
		if msg == "" {
			tray.SetTooltip(trayTip)
			return
		}
		tray.SetTooltip("0type — " + msg)
		return
	}
	runtime.EventsEmit(a.ctx, "notice", map[string]string{"kind": kind, "msg": msg})
}

// GetSettings returns the current settings (the frontend calls this on load).
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// DefaultModels returns the OpenRouter model used when the cleanup model field
// is left blank (shown as the input's placeholder).
func (a *App) DefaultModels() map[string]string {
	return map[string]string{
		"cleanup": cleanup.DefaultCloudModel,
	}
}

// Recommendations returns OpenRouter model shortlists for the cleanup model
// field, refreshed from the rankings feeds at most once a day (or now, with refresh),
// from the cache or the embedded snapshot when offline.
func (a *App) Recommendations(refresh bool) (recommend.Result, error) {
	return recommend.Get(models.Dir(), refresh)
}

// SaveSettings persists the settings edited in the window and applies the mode,
// microphone, output style, and backends live. The trigger applies live via
// CaptureBinding; model downloads apply live via DownloadParakeet and
// DownloadQwen.
func (a *App) SaveSettings(s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	if a.engine != nil {
		a.engine.SetMode(s.Mode)
		a.engine.SetInputDevice(s.InputDevice)
		a.engine.SetLive(s.live())
		a.engine.SetNotes(s.Notes)
		a.engine.SetCleanup(s.Cleaner, s.OpenRouterKey, s.CleanupModel)
	}
	return a.save()
}

// GetVersion returns the app version for the window footer.
func (a *App) GetVersion() string { return Version }

// InputDevices lists the available microphone names ("" = system default is
// implicit and always offered by the UI).
func (a *App) InputDevices() []string { return audio.InputDevices() }

// GetAutostart reports whether 0type launches at Windows login.
func (a *App) GetAutostart() bool { return autostart.Enabled() }

// SetAutostart enables or disables launching at Windows login.
func (a *App) SetAutostart(on bool) error { return autostart.SetEnabled(on) }

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
