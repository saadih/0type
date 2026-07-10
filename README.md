# 0type

**Zero typing.** Hold a button, talk, and your words land — cleaned up — in whatever app you're looking at. Local-first, cross-platform, and small.

> Built because every dictation tool either can't bind to a mouse button, phones home, or ships a kitchen sink of features around a core loop it forgot to polish. 0type does one thing: **hold → talk → text.** Everywhere.

## Why

- **Bind to your mouse.** Real global push-to-talk on a mouse side button (MB4/MB5) — the thing Electron's `globalShortcut` can't do and half the field gets wrong. Rebind to any key or button.
- **Local-first.** Transcription (Parakeet) and cleanup (a small LLM) run on your machine. Cloud is opt-in, never required.
- **Fast.** Parakeet is ~10× faster than Whisper and doesn't hallucinate in your pauses. It feels instant.
- **Small on purpose.** One binary, a tray icon, a settings window. No accounts, no sync, no "workflows."

## How it works

```
MB4 down ──▶ HUD "listening" ──▶ mic capture (PCM buffer)
   │                                      │
MB4 up ◀──────────────────────────────────┘
   │
   ▼
Parakeet v3 (sherpa-onnx) ──▶ raw transcript
   │
   ▼
[cleanup on?] ──▶ Qwen3.5 4B-instruct (local) ──▶ cleaned text
   │
   ▼
inject at cursor (clipboard-paste) ──▶ HUD dismiss
```

Every stage is an interface (`internal/*`), wired in [`cmd/0type`](cmd/0type). The skeleton runs today with stdlib-only stubs — real implementations drop in behind the interfaces.

## Stack

| Concern | Choice |
|---|---|
| Language / UI | Go + [Wails](https://wails.io) (real settings window, single binary) |
| Global hook + inject | [robotgo](https://github.com/go-vgo/robotgo) (mouse buttons + keyboard, cross-platform) |
| Microphone | miniaudio (malgo) |
| Transcription (ASR) | [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) running **Parakeet TDT 0.6B v3** |
| Cleanup (LLM) | **Qwen3.5 4B Instruct**, local via Ollama (low temp, no thinking) |
| Tray | getlantern/systray |

## The three decisions

- **ASR → Parakeet TDT 0.6B v3.** Beats Whisper on English WER (6.32% vs 7.44%), ~10× faster, barely hallucinates during silence (critical for pause-heavy dictation), and covers 25 European languages incl. Swedish.
- **Cleanup → Qwen3.5 4B Instruct.** Best small local model for this in 2026. Run the *instruct* (non-thinking) variant at low temperature so it tidies without rewriting. Gemma 3 4B is the multilingual runner-up worth A/B testing on Swedish.
- **Trigger → MB4 (mouse back button), hold-to-talk, fully rebindable.** The origin story and the headline feature. Toggle mode ships too, for long-form.

## Status

Pre-alpha, built in the open.

### Roadmap

- **v0 (loop):** tray + global hook on one key + mic + cloud ASR stub + clipboard-paste inject. Prove hold → talk → text.
- **v1 (MVP):** local Parakeet v3 · Qwen3.5 cleanup · settings UI with the rebindable key/mouse-button picker · hold + toggle modes · model pre-warm · recording HUD.
- **Out of scope (on purpose):** history, custom vocab / auto-learn, profiles, cloud sync, workflows, model marketplace. The core loop stays flawless before anything else exists.

## Build

```sh
go run ./cmd/0type   # runs the skeleton — press Enter to simulate a dictation
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design and the engineering gotchas that separate "works" from "feels instant."

## License

MIT © 2026 Hussein Al-Saadi
