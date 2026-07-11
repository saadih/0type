# Architecture

0type is a push-to-talk dictation tool built around one loop:

```
hold a button → record → transcribe → clean up → paste at the cursor
```

The rest of the code exists to make that loop quick and work in every app.

## Pipeline

The console (`cmd/0type`) and the GUI (repo root) share one engine, `internal/app.Engine`. Each stage is a small interface, so backends swap without touching the wiring:

| Stage | Package | Implementation |
|---|---|---|
| Trigger | `internal/hotkey` | Low-level mouse + keyboard hook, rebindable |
| Record | `internal/audio` | winmm `waveIn`, 16 kHz mono PCM, no CGO |
| Transcribe | `internal/transcribe` | Parakeet via sherpa-onnx (cgo), or Groq, or a stub |
| Clean | `internal/cleanup` | Qwen via an OpenAI-compatible endpoint |
| Inject | `internal/inject` | Clipboard write, paste, restore |
| Overlay | `internal/overlay` | Cursor dot: red recording, blue processing, raw Win32 |

The engine runs three goroutines: the trigger's hook thread, a worker that starts and stops recording, and a single output worker that transcribes, cleans, and injects one recording at a time.

## Models

**Transcription: Parakeet TDT 0.6B v3** (`sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8`)

- Runs about 10x faster than Whisper large-v3, under a second on short clips.
- Lower English word error rate (6.32% vs 7.44%), and it doesn't hallucinate text during silence. Dictation is full of pauses, so that matters.
- Covers 25 European languages including Swedish, at the size of the English-only v2.
- Runs in-process through `sherpa-onnx-go` (CGO). It sits behind the `parakeet` build tag, so the default build stays CGO-free.

**Cleanup: Qwen3-4B-Instruct-2507** (Q4_K_M GGUF)

- 0type downloads a `llama-server` binary and the GGUF, spawns the server itself (`--jinja -ngl 99`), and points the cleaner at it. A server already running on the port gets reused.
- The Instruct build has no thinking mode, so cleanup never wastes its budget on a `<think>` trace. Requests still send `enable_thinking:false` as a guard for anyone who points the cleaner at a hybrid-reasoning model instead.
- Cleanup runs at temperature 0.2 so it tidies instead of rewriting.
- The first request processes the ~400-token system prompt (about 11s cold), then the prefix caches and later requests take about 0.4s. 0type prewarms it on startup.
- The system prompt lives in [`internal/cleanup/prompt.txt`](../internal/cleanup/prompt.txt).

## Trigger

- The default binding is the mouse back button (MB4), hold to talk. That's the reason the project exists.
- Rebind it to any key or mouse side/middle button, and the change applies live. Electron's `globalShortcut` can't bind a mouse button at all.
- The hook installs one low-level mouse hook and one low-level keyboard hook, then matches whatever binding is current. Rebinding swaps the target under a lock, so nothing reinstalls.
- Reading which button fired means reading the OS hook struct from `lParam`. `go vet` flags that conversion. It's the standard WinAPI pattern; see CONTRIBUTING.

## What makes it feel quick

1. **Paste, don't type.** Inject saves the clipboard, writes the text, sends Ctrl+V, then restores the clipboard. Simulating keystrokes is slow and mangles Swedish characters and emoji.
2. **Prewarm the cleanup cache.** The engine fires a throwaway cleanup on startup, so the first real dictation reuses the cached system prompt.
3. **Keep the hook thread free.** The hook callback only sends on a channel. Recording, transcription, and cleanup run on other goroutines, so a slow step never lags the system mouse.
4. **One output worker.** Recordings queue through a single goroutine, so two quick dictations can't fight over the clipboard or land out of order.

## The overlay

A raw Win32 layered window shows a dot that follows the cursor: red while the mic is live, blue while the recording is transcribed and cleaned. It's built `WS_EX_NOACTIVATE | WS_EX_TRANSPARENT`, so it never takes focus. If it did, the paste would land on the overlay instead of your document. The engine drives the color from a small state machine where recording outranks processing, so a quick second dictation stays red instead of being cleared when the previous job finishes.

## The tray

Closing the window doesn't quit 0type. `OnBeforeClose` hides the window and returns true, so the app keeps listening from the notification area. `internal/tray` is another raw Win32 window on its own thread; it owns a `Shell_NotifyIcon` and a right-click menu (Open, Quit). Quit sets a flag so the next `OnBeforeClose` lets the app exit for real.

## Distribution

Nothing heavy lives in git. `internal/models` downloads the GGUF, the Parakeet model, and the `llama-server` binary on demand into `%LOCALAPPDATA%\0type\`, streaming each to a `.part` file and renaming on success. The sherpa DLLs come from the Go module cache; `scripts/build-parakeet.ps1` copies them next to the exe.

## Out of scope

History, custom vocabulary, multiple profiles, cloud sync, workflows, and model marketplaces stay out. The loop comes first.

## Credits

The cleanup prompt in `internal/cleanup/prompt.txt` is adapted from [OpenWhispr](https://github.com/OpenWhispr/openwhispr) (MIT), with a language rule added and the wake-word placeholder removed.
