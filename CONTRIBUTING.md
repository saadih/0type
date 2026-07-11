# Contributing to 0type

Thanks for your interest! 0type is small on purpose — the goal is a fast,
local, native push-to-talk dictation tool that does one loop flawlessly:
**hold → talk → clean text at your cursor.** Contributions that sharpen that
loop (or extend it without bloating it) are very welcome.

## Ground rules

- **Keep it lean.** Before adding a feature, ask whether it belongs in the core
  loop. History, cloud sync, accounts, "workflows", model marketplaces — out of
  scope by design.
- **Local-first.** New transcription/cleanup backends should be able to run on
  the user's machine. Cloud is a fallback, never a requirement.
- **Nothing heavy in the repo.** Models and native binaries are downloaded on
  demand into the user's data dir — never committed.

## Prerequisites

- [Go](https://go.dev/) 1.23+
- [Node.js](https://nodejs.org/) + the Wails CLI:
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 (preinstalled on Windows 11)
- **For the Parakeet (CGO) build only:** a C toolchain —
  `winget install BrechtSanders.WinLibs.POSIX.UCRT`, then reopen your terminal.

## Project layout

```
cmd/0type/        headless console build of the engine
main.go, app.go   the Wails GUI (window + settings + model downloads)
frontend/         the settings UI (vanilla JS + Vite)
internal/
  app/            the shared dictation Engine
  hotkey/         global keyboard+mouse hook, rebinding, capture
  audio/          winmm microphone capture
  transcribe/     Parakeet (cgo) | Groq | stub
  cleanup/        Qwen via an OpenAI-compatible endpoint
  inject/         clipboard-paste
  overlay/        floating recording dot
  models/         on-demand downloads + bundled llama-server
docs/ARCHITECTURE.md   design + the "feels instant" gotchas
```

Almost everything is an interface with a Windows implementation and a non-Windows
stub, so the module builds everywhere even though the app is Windows-first.

## Dev workflow

```powershell
wails dev                     # hot-reload the GUI
go run ./cmd/0type            # headless engine, no window
go build ./... ; go vet ./... # default (CGO-free) build must stay green
pwsh scripts/build-parakeet.ps1   # the full local-transcription build
```

**Before opening a PR:**

- `go build ./...` and `go vet ./...` pass on the default (CGO-free) build.
- If you touched the Parakeet path, `pwsh scripts/build-parakeet.ps1` still builds.
- Verify behavior by actually driving the loop (the `verify` mindset), not just
  compiling.

### Expected `go vet` notes

`internal/hotkey/manager_windows.go` has two `possible misuse of unsafe.Pointer`
notes. These are the **necessary, standard WinAPI idiom** for reading the
low-level hook structs from `lParam`; they are not defects. Please don't "fix"
them by weakening the hook.

## Adding a backend

- **A transcriber** implements `transcribe.Transcriber` (`Transcribe(wav []byte)
  (string, error)`, input is a 16 kHz mono 16-bit WAV). Wire it in
  `internal/app/engine.go`.
- **A cleaner** implements `cleanup.Cleaner`. The default talks to an
  OpenAI-compatible chat endpoint.
- **A downloadable model/binary** is an `models.Asset`; add a constructor to
  `internal/models/registry.go` with an **official, stable URL**.

Native-heavy backends should live behind a build tag (see the `parakeet` tag) so
the default build stays CGO-free.

## Commits & PRs

- Small, focused commits with a clear message (what changed and why).
- One logical change per PR; describe how you verified it.
- Match the surrounding code's style and comment density.

## License

By contributing, you agree your contributions are licensed under the project's
[MIT license](LICENSE).
