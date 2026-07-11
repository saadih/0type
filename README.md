# 0type

> no typing allowed

Hold a button, talk, and your cleaned-up words appear in whatever app you're using. 0type binds push-to-talk dictation to your mouse, runs speech recognition and text cleanup on your own machine, and pastes into any window. Nothing leaves your computer.

```
hold trigger → record → Parakeet (local) → Qwen (local) → paste at cursor
```

## Why

Most dictation tools can't bind to a mouse button, ship your audio to a server, or wrap a simple loop in features you never asked for. 0type keeps the loop small:

- **Binds to your mouse.** Global push-to-talk on a side button (MB4/MB5), which Electron's `globalShortcut` can't reach. Rebind it live to any key or button.
- **Runs on your machine.** Parakeet handles transcription, Qwen 3.5 4B handles cleanup, both downloaded on demand.
- **Small.** An 11 MB native binary over WebView2. Electron apps run ten times that. The hook, audio capture, and overlay are plain Go with no bundled browser.
- **Focused.** One window, a recording dot, a few settings.

## What it does

| Stage | How |
|---|---|
| Trigger | Global low-level hook, rebindable to any key or mouse side/middle button, applied live |
| Capture | Microphone via `winmm` (no CGO) |
| Transcribe | Parakeet TDT 0.6B v3 via [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx), 25 European languages including Swedish |
| Clean up | Qwen 3.5 4B via a bundled [llama.cpp](https://github.com/ggml-org/llama.cpp) server: drops filler, fixes punctuation, keeps your wording |
| Inject | Clipboard paste, which handles å/ä/ö and emoji |
| Feedback | A red dot that follows your cursor while recording |

You download the models from HuggingFace and GitHub releases into `%LOCALAPPDATA%\0type\`. They never touch this repo or the binary.

## Install and run

> Windows 10/11 (x64). The module compiles on other platforms, but the hook, audio, and overlay only work on Windows.

Download the [latest release](https://github.com/saadih/0type/releases/latest), unzip it, and keep the DLLs next to `0type.exe`. Then:

1. Run `0type.exe`.
2. Open Models and download Parakeet (about 600 MB) and Qwen (about 2.7 GB).
3. Restart. Hold your mouse back button (MB4), speak, and release.

The dot trails your cursor while you talk; the text lands where you were typing. Rebind the trigger to whatever you want in settings.

Windows may warn on first launch because the build isn't signed. Click More info, then Run anyway. To build it yourself instead, see [Build from source](#build-from-source).

## Build from source

You need:

- [Go](https://go.dev/) 1.23+
- [Node.js](https://nodejs.org/) and the [Wails CLI](https://wails.io/): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 (ships with Windows 11)
- A C toolchain, for the local Parakeet build only: `winget install BrechtSanders.WinLibs.POSIX.UCRT`, then reopen your terminal

Two builds:

```powershell
# CGO-free, no local transcription (dev/CI, or a cloud fallback)
wails build

# Local Parakeet transcription (CGO + sherpa-onnx), copies its DLLs
pwsh scripts/build-parakeet.ps1
```

Both write `build\bin\0type.exe`. The Parakeet build also drops `onnxruntime.dll` and `sherpa-onnx-c-api.dll` next to it. To test the pipeline headless, run `go run ./cmd/0type`.

## Configure

- **Trigger:** click Rebind, then press any key or mouse button. Pick something you don't type, like an F-key, a side button, Right Ctrl, or Caps Lock.
- **Mode:** hold to talk, or tap to toggle.
- **Models:** download or re-download Parakeet and Qwen.

## How it's built

One Go module. The console and the GUI share the engine in `internal/app`; each stage is a small interface you can swap:

```
internal/
  hotkey/     global keyboard+mouse hook, rebinding, capture   (raw Win32)
  audio/      winmm microphone capture -> WAV                  (raw Win32)
  transcribe/ Parakeet (sherpa-onnx, cgo) | Groq | stub
  cleanup/    Qwen via an OpenAI-compatible endpoint
  inject/     clipboard paste                                  (raw Win32)
  overlay/    floating recording dot                           (raw Win32)
  models/     on-demand downloads + bundled llama-server
```

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) covers the design and the details that keep it quick.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © 2026 Hussein Al-Saadi

The cleanup prompt is adapted from [OpenWhispr](https://github.com/OpenWhispr/openwhispr) (MIT).
