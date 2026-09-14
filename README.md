# 0type

> no typing allowed

Hold a button, talk, and your cleaned-up words appear in whatever app you're using. 0type binds push-to-talk dictation to your mouse, runs speech recognition and text cleanup on your own machine, and pastes into any window. Your voice never leaves your computer. Only the cleaned-up text does, and only if you point cleanup at a cloud model.

```
hold trigger → record → Parakeet (local) → Qwen (local) → paste at cursor
                 ↳ at every pause, the piece so far is transcribed, cleaned, and pasted
```

## Why

Most dictation tools can't bind to a mouse button, ship your audio to a server, or wrap a simple loop in features you never asked for. 0type keeps the loop small:

- **Binds to your mouse.** Global push-to-talk on a side button (MB4/MB5), which Electron's `globalShortcut` can't reach. Rebind it live to any key or button.
- **Runs on your machine.** Parakeet handles transcription, Qwen3-4B-Instruct handles cleanup, both downloaded on demand. Transcription is always local, so your audio never leaves. If cleanup is sluggish on your hardware, or you want a bigger model catching misheard words, point that one stage at OpenRouter with your own key.
- **Pastes as you speak.** Each pause becomes a paste, so a long dictation lands while you talk instead of after. Recordings have no length limit.
- **Small.** An 11 MB native binary over WebView2. Electron apps run ten times that. The hook, audio capture, and overlay are plain Go with no bundled browser.
- **Focused.** One window that tucks into the system tray, a cursor dot, a few settings.

## What it does

| Stage | How |
|---|---|
| Trigger | Global low-level hook, rebindable to any key or mouse side/middle button, applied live |
| Capture | Microphone via `winmm` (no CGO), any length; pauses split it into pieces on the fly |
| Transcribe | Parakeet TDT 0.6B v3 via [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx), 25 European languages including Swedish. Always local |
| Clean up | Qwen3-4B-Instruct via a bundled [llama.cpp](https://github.com/ggml-org/llama.cpp) server: drops filler, fixes punctuation, repairs misheard words ("I say at the computer" → "I sit at the computer"), keeps your wording; or any model on OpenRouter |
| Inject | Clipboard paste, which handles å/ä/ö and emoji |
| Feedback | A dot that follows your cursor: red while recording, blue while it transcribes and cleans up |

You download the models from HuggingFace and GitHub releases into `%LOCALAPPDATA%\0type\`. They never touch this repo or the binary.

## Install and run

> Windows 10/11 (x64). The module compiles on other platforms, but the hook, audio, and overlay only work on Windows.

Download the [latest release](https://github.com/saadih/0type/releases/latest) and run **`0type-amd64-installer.exe`**. It installs for your user (no admin), adds a Start Menu shortcut, and checks for the WebView2 runtime. Prefer no installer? Grab the portable `.zip` from the same page and keep the DLLs next to `0type.exe`.

Then:

1. Launch 0type.
2. Open Models and download Parakeet (about 600 MB) and Qwen (about 2.5 GB).
3. Hold your mouse back button (MB4), speak, and release.

The dot trails your cursor while you talk, red while recording and blue while it works; the text lands where you were typing. Rebind the trigger to whatever you want, and close the window to tuck 0type into the tray.

Windows may warn on first launch because the build isn't signed. Click More info, then Run anyway. To build it yourself, see [Build from source](#build-from-source).

## Build from source

You need:

- [Go](https://go.dev/) 1.23+
- [Node.js](https://nodejs.org/) and the [Wails CLI](https://wails.io/): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 (ships with Windows 11)
- A C toolchain, for the local Parakeet build only: `winget install BrechtSanders.WinLibs.POSIX.UCRT`, then reopen your terminal

Three builds:

```powershell
# CGO-free, so this build cannot transcribe at all (dev/CI only)
wails build

# Local Parakeet transcription (CGO + sherpa-onnx), copies its DLLs
pwsh scripts/build-parakeet.ps1

# The single-file per-user installer (needs NSIS: winget install NSIS.NSIS)
pwsh scripts/build-installer.ps1
```

The first two write `build\bin\0type.exe`; the Parakeet build also drops the sherpa DLLs next to it. The installer script produces `build\bin\0type-amd64-installer.exe`. To test the pipeline headless, run `go run ./cmd/0type`.

## Configure

The main screen holds what you touch while dictating:

- **Trigger:** click Rebind, then press any key or mouse button. Pick something you don't type, like an F-key, a side button, Right Ctrl, or Caps Lock.
- **Cleanup:** local Qwen or OpenRouter. The local option appears once the model is downloaded in Settings. Cloud needs an [OpenRouter API key](https://openrouter.ai/keys), and your transcripts then leave your machine. It is a fallback for hardware that struggles with a 4B model, not a speed-up: on anything that runs the local model comfortably the network round trip costs more time than the bigger model saves. The model field has a Recommended picker fed by [OpenRouter's rankings](https://openrouter.ai/rankings) (best value, fastest, smartest), refreshed daily and available offline from a bundled copy.
- **About you:** a short note to the cleanup model. Names and words to spell right, preferences to follow. Sent with every cleanup request, so it goes to OpenRouter when cleanup is in the cloud.

Settings (top right) holds the rest:

- **Mode:** hold to talk, or tap to toggle.
- **Output:** paste as you speak (each pause becomes a paste), or paste once when you stop.
- **Microphone:** use the system default or pick a specific input device.
- **Start with Windows:** launch 0type at login.
- **Models:** download or re-download Parakeet and Qwen.
- **My models:** your own OpenRouter slugs, listed first in the Recommended picker.

Keys are saved in `%APPDATA%\0type\config.json`, readable only by your Windows account.

## How it's built

One Go module. The console and the GUI share the engine in `internal/app`; each stage is a small interface you can swap:

```
internal/
  hotkey/     global keyboard+mouse hook, rebinding, capture   (raw Win32)
  audio/      winmm microphone capture -> WAV                  (raw Win32)
  transcribe/ Parakeet (sherpa-onnx, cgo) | stub
  cleanup/    Qwen via an OpenAI-compatible endpoint
  inject/     clipboard paste                                  (raw Win32)
  overlay/    cursor dot: red recording, blue processing       (raw Win32)
  tray/       system tray icon + Open/Quit menu                (raw Win32)
  autostart/  run-at-login toggle (HKCU Run key)
  models/     on-demand downloads + bundled llama-server
```

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) covers the design and the details that keep it quick.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © 2026 Hussein Al-Saadi

The cleanup prompt is adapted from [OpenWhispr](https://github.com/OpenWhispr/openwhispr) (MIT).
