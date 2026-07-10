# Architecture

0type is a push-to-talk dictation tool. The entire product is one loop:

**hold a button → speak → transcribe → clean up → paste at the cursor.**

Everything else is in service of making that loop feel instant and work in every app.

## Pipeline

```
trigger (hold) ─▶ record mic ─▶ transcribe (ASR) ─▶ clean (LLM) ─▶ inject
```

Each stage is a small interface in `internal/`, so implementations swap without
touching the wiring in `cmd/0type/main.go`:

| Stage | Package | Interface | MVP implementation |
|---|---|---|---|
| Trigger | `internal/hotkey` | `Trigger` | robotgo global hook (mouse buttons + keys) |
| Record | `internal/audio` | `Recorder` | miniaudio (malgo), 16 kHz mono PCM |
| Transcribe | `internal/transcribe` | `Transcriber` | sherpa-onnx + Parakeet TDT 0.6B v3 |
| Clean | `internal/cleanup` | `Cleaner` | Qwen3.5 4B Instruct via Ollama |
| Inject | `internal/inject` | `Injector` | clipboard write + paste + restore |

The committed skeleton ships stdlib-only stubs for every stage, so the pipeline
runs and prints end-to-end before any native dependency is added. Build order:
prove the loop with stubs → swap real impls one at a time, from `inject` backwards
(inject is the most satisfying to see work, and it's the cheapest to verify).

## Model choices

**ASR — Parakeet TDT 0.6B v3** (`sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8`)
- ~10× faster than Whisper large-v3; sub-second on short clips even on CPU.
- Lower English WER (6.32% vs 7.44%) and, crucially, it does **not** hallucinate
  text during silence — dictation is full of pauses.
- Covers 25 European languages including Swedish, at the same size as English-only v2.

**Cleanup — Qwen3.5 4B Instruct** (local, via Ollama)
- Use the **instruct / non-thinking** variant. Reasoning tokens add latency for a
  task that needs none.
- Run at **low temperature (~0.2)**. Cleanup should tidy, not rewrite or invent.
- Gemma 3 4B is the multilingual runner-up — A/B test it on Swedish dictation.
- System prompt lives in [`prompts/cleanup.txt`](../prompts/cleanup.txt).

## Trigger

- **Default: mouse back button (MB4), hold-to-talk.** This is why the project exists.
- **Fully rebindable** to any key or mouse button — the capability Electron's
  `globalShortcut` lacks and the feature that sets 0type apart.
- **Two modes:** *hold* (default, for a sentence) and *toggle* (tap on/off, for
  long-form so you're not physically holding a button for minutes).
- **Keyboard fallback default: Right Ctrl.** Avoid Right Alt — it's AltGr on
  international / Swedish layouts.
- **First thing to de-risk:** confirm the global hook actually reports mouse side
  buttons (MB4/MB5) on Windows, not just left/right/middle. The whole idea rests on it.

## Engineering gotchas (works vs. feels instant)

1. **Inject via clipboard-paste, not simulated typing.** Save clipboard → set text →
   send Ctrl/Cmd+V → restore clipboard. Char-by-char typing is slow and mangles
   Unicode (Swedish å/ä/ö, emoji). Biggest single "feel" detail.
2. **Pre-warm both models on launch.** Load Parakeet and the LLM into memory at
   startup so the first dictation isn't a multi-second stall.
3. **Never block the hook callback.** ASR and LLM run on worker goroutines; the
   input hook stays instant. Go's concurrency makes this trivial.
4. **Cleanup is the slowest link on CPU.** Offer a "raw / fast" mode
   (`cleanup.Noop`) that skips the LLM — Parakeet alone is near-instant.

## UI (Wails)

0type is a **tray app**, not a window you keep open.
- Tray menu: enable/disable · settings · quit.
- Settings window: the key-binding recorder (press your button/key to bind, mouse
  buttons included), mode (hold/toggle), language, cleanup on/off + endpoint.
- Recording HUD: a small floating pill near the cursor — *listening → transcribing*.
  80% of the premium feel; ship in MVP if time allows, else v1.1.

## Out of scope (deliberately)

History, custom vocabulary / auto-learn, multiple profiles, cloud sync,
"workflows," model marketplaces. The core loop stays flawless before any of it is
considered. Scope discipline is the competitive advantage.

## Credits

The cleanup system prompt in `prompts/cleanup.txt` is adapted from
[OpenWhispr](https://github.com/OpenWhispr/openwhispr) (MIT), with a
language-preservation rule added and the wake-word placeholder removed.
