# Contributing to 0type

0type stays small on purpose. It does one loop: hold a button, talk, get clean text at your cursor. Changes that sharpen that loop, or extend it without bloating it, are welcome.

## Ground rules

- **Keep it lean.** Before adding a feature, ask whether it belongs in the core loop. History, cloud sync, accounts, workflows, and model marketplaces stay out.
- **Run locally.** A new transcription or cleanup backend should run on the user's machine. Cloud is a fallback, not a requirement.
- **Keep the repo light.** Models and native binaries download on demand into the user's data dir. Don't commit them.

## Prerequisites

- [Go](https://go.dev/) 1.23+
- [Node.js](https://nodejs.org/) and the Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 (ships with Windows 11)
- A C toolchain for the Parakeet build: `winget install BrechtSanders.WinLibs.POSIX.UCRT`, then reopen your terminal

## Project layout

```
cmd/0type/        headless console build of the engine
main.go, app.go   the Wails GUI (window, settings, model downloads)
frontend/         the settings UI (vanilla JS + Vite)
internal/
  app/            the shared dictation engine
  hotkey/         global keyboard+mouse hook, rebinding, capture
  audio/          winmm microphone capture
  transcribe/     Parakeet (cgo) | Groq | stub
  cleanup/        Qwen via an OpenAI-compatible endpoint
  inject/         clipboard paste
  overlay/        floating recording dot
  models/         on-demand downloads + bundled llama-server
docs/ARCHITECTURE.md   design and the details that keep it quick
```

Most stages have a Windows implementation and a non-Windows stub, so the module builds everywhere even though the app targets Windows.

## Dev workflow

```powershell
wails dev                         # hot-reload the GUI
go run ./cmd/0type                # headless engine, no window
go build ./... ; go vet ./...     # the default (CGO-free) build stays green
pwsh scripts/build-parakeet.ps1   # the full local-transcription build
```

Before you open a PR:

- `go build ./...` and `go vet ./...` pass on the default (CGO-free) build.
- If you touched the Parakeet path, `pwsh scripts/build-parakeet.ps1` still builds.
- Drive the loop and watch it work. A clean compile isn't enough.

### Expected go vet notes

`internal/hotkey/manager_windows.go` reports two "possible misuse of unsafe.Pointer" notes. They read the low-level hook structs from `lParam`, which is the standard WinAPI pattern. Leave them, and don't weaken the hook to silence them.

## Adding a backend

- A transcriber implements `transcribe.Transcriber`: `Transcribe(wav []byte) (string, error)`, where the input is a 16 kHz mono 16-bit WAV. Wire it into `internal/app/engine.go`.
- A cleaner implements `cleanup.Cleaner`. The default one talks to an OpenAI-compatible chat endpoint.
- A downloadable model or binary is a `models.Asset`. Add a constructor to `internal/models/registry.go` with an official, stable URL.

Put native-heavy backends behind a build tag, the way the `parakeet` tag works, so the default build stays CGO-free.

## Commits and PRs

- Small, focused commits. Say what changed and why.
- One logical change per PR. Describe how you tested it.
- Match the surrounding code's style and comment density.

## License

By contributing, you agree your work is licensed under the project's [MIT license](LICENSE).
