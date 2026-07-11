# 0type

> **no typing allowed**

Hold a button, talk, and your words land — cleaned up — in whatever app you're looking at. Fully local, native, and small. Push-to-talk dictation that binds to **your mouse**, runs speech recognition and cleanup **on your own machine**, and pastes into any app.

No accounts. No subscription. No cloud. No keys.

```
hold trigger → 🎙 record → Parakeet (local) → Qwen (local) → paste at cursor
```

---

## Why it exists

Every dictation tool either can't bind to a mouse button, phones home, or buries a simple core loop under a kitchen sink of features. 0type does one thing well:

- **Bind to your mouse.** Real global push-to-talk on a mouse side button (MB4/MB5) — the thing Electron's `globalShortcut` can't do. Rebind live to *any* key or mouse button.
- **Fully local.** Transcription (NVIDIA **Parakeet**) and cleanup (**Qwen 3.5 4B**) run on your machine. Download the models in-app; nothing is sent anywhere.
- **Fast & native.** A ~11 MB Wails binary over WebView2 — not a 150 MB Electron app. Pure-Go global hook, audio capture, and overlay; no bundled Chromium.
- **Small on purpose.** One window, a floating recording dot, a couple of settings. That's it.

## What it does

| Stage | How |
|---|---|
| **Trigger** | Global low-level hook; rebindable to any key or mouse side/middle button, applied live |
| **Capture** | Microphone via `winmm` (no CGO) |
| **Transcribe** | **Parakeet TDT 0.6B v3** locally via [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) — 25 European languages incl. Swedish |
| **Clean up** | **Qwen 3.5 4B** locally via a bundled [llama.cpp](https://github.com/ggml-org/llama.cpp) server — removes filler, fixes punctuation, keeps your voice |
| **Inject** | Clipboard-paste (Unicode-safe: å/ä/ö, emoji) |
| **Feedback** | A tiny red dot that follows your cursor while recording |

Models are **downloaded on demand** from official sources (HuggingFace, GitHub releases) into `%LOCALAPPDATA%\0type\` — never committed to this repo, never bundled in the binary.

## Install & run

> Windows 10/11 (x64). Other platforms build but the global hook, audio, and overlay are Windows-only for now.

**From source** (see [Build](#build-from-source)), then:

1. Run `0type.exe`.
2. In **Models**, click **Download** on Parakeet (~600 MB) and Qwen (~2.7 GB).
3. **Restart.** That's it — hold your mouse back button (MB4), speak, release.

The recording dot follows your cursor; your words appear where you're typing. Rebind the trigger to anything in the settings window.

## Build from source

**Prerequisites**

- [Go](https://go.dev/) 1.23+
- [Node.js](https://nodejs.org/) + the [Wails CLI](https://wails.io/): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 (preinstalled on Windows 11)
- **For local Parakeet only:** a C toolchain — `winget install BrechtSanders.WinLibs.POSIX.UCRT` (then reopen your terminal)

**Two builds**

```powershell
# Default build — CGO-free, no local transcription (dev/CI, or cloud fallback)
wails build

# Full build — local Parakeet transcription (CGO + sherpa-onnx), ships its DLLs
pwsh scripts/build-parakeet.ps1
```

Both produce `build\bin\0type.exe`. The Parakeet build additionally copies `onnxruntime.dll` + `sherpa-onnx-c-api.dll` next to it.

There's also a headless console build for quick testing: `go run ./cmd/0type`.

## Configure

- **Trigger** — click *Rebind*, then press any key or mouse button. Applied instantly. Pick something you don't type (F-keys, side buttons, Right Ctrl, Caps Lock).
- **Mode** — hold-to-talk (default) or tap-to-toggle.
- **Models** — download / re-download Parakeet and Qwen.

## How it's built

A single Go module. The dictation engine (`internal/app`) is shared by the console and the GUI; each stage is a small swappable interface:

```
internal/
  hotkey/    global keyboard+mouse hook, rebinding, capture   (raw Win32)
  audio/     winmm microphone capture -> WAV                  (raw Win32)
  transcribe/ Parakeet (sherpa-onnx, cgo) | Groq | stub
  cleanup/   Qwen via an OpenAI-compatible endpoint
  inject/    clipboard-paste                                  (raw Win32)
  overlay/   floating recording dot                           (raw Win32)
  models/    on-demand downloads + bundled llama-server
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design and the gotchas that separate "works" from "feels instant."

## Contributing

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © 2026 Hussein Al-Saadi

The cleanup prompt is adapted from [OpenWhispr](https://github.com/OpenWhispr/openwhispr) (MIT).
