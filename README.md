# JARVIS

JARVIS is a local-first AI desktop assistant for Windows, built with Go, [Wails 2](https://wails.io), Vue 3, and [Ollama](https://ollama.com). It combines a chat window with OS-level commands: screen analysis, app launching, volume control, file operations, notes, and system actions.

The AI model runs on your machine through Ollama. JARVIS has no telemetry, no account system, and no cloud AI backend. Optional network features are weather/geolocation (Open-Meteo), web search (DuckDuckGo), and image generation (Pollinations.ai).

> Status: the project builds and the command layer is covered by tests. There is no signed installer or automatic updater yet. Build from source using the instructions below.

## Features

- **Local AI chat** through any Ollama model you have pulled.
- **Persistent chat history** restored on startup, with the last 40 user/assistant messages kept locally and encrypted with Windows DPAPI.
- **Streaming chat**: free-form replies appear progressively over Wails events, and Stop cancels generation. Slash commands remain synchronous.
- **Screen analysis** with a vision-capable model (for example `llava`). A screenshot is only captured after you approve it in the UI.
- **System commands**: application launching, volume control, screenshots, shutdown, and restart.
- **File commands** restricted to an allowlist: Desktop, Documents, and the JARVIS config folder.
- **Notes** persisted locally and encrypted with Windows DPAPI.
- **Web search**: DuckDuckGo results are fetched through an address-validated HTTP client (the headless-browser path additionally uses a loopback egress proxy), sanitized, and summarized by the local model.
- **Image generation** through Pollinations.ai, disabled until you explicitly opt in in Settings.
- **System tray** with Show / Hide / Quit, and a global `Ctrl+Shift+J` hotkey. Closing the window hides JARVIS to the tray; the tray **Quit** item is the only real exit.
- **Spoken phrases** for actions, with an on/off toggle in Settings.
- **Russian & English voice control**: local Vosk wake words («привет Джарвис» / «hey Jarvis»), bilingual Whisper command recognition, microphone selection, push-to-talk, and a built-in microphone health check.
- **Autostart**: an optional logon entry starts the full application hidden in the tray, so the wake word, hotkey and tray icon are available immediately. Legacy supervised-agent entries are migrated automatically.
- **Proactive assistant**: a startup briefing (time, weather, next reminder, current game), reminders and timers that fire while JARVIS is running, and a local-model watchdog. Unprompted messages respect quiet hours (23:00–08:00) and the autonomy level in Settings.
- **Game awareness**: detects the foreground game without Steam, remembers titles you confirm, tracks session and daily playtime locally, and offers a break after an hour. Detection is read-only — no injection, memory reads, or input automation.
- **Local weather**: Open-Meteo forecasts with a one-time location prompt, IP geolocation, and Russian city-name normalization.
- **Audit log** of actions and confirmations, stored locally and pruned after 30 days.
- **DPAPI-encrypted settings, history, and notes** at rest.
- **Optional voice-engine checksum verification** with a generated `checksums.json` manifest.

## Requirements

- Windows 10 or 11 (x64).
- [Ollama](https://ollama.com/download) installed and running.
- An Ollama model, for example `ollama pull llama3.1:8b`. Small machines can use `llama3.2:3b`; screen analysis needs a vision model such as `ollama pull llava:7b`.
- For building: Go 1.26.x (1.26.10 is tested; Go 1.27 currently breaks the Wails build), Node.js 20.19+ (or 22.12+), npm, and the Wails CLI.
- WebView2 runtime (included with current Windows 10/11 installations).

## Quick start

1. Install and start Ollama, then pull a model:

   ```powershell
   ollama pull llama3.1:8b
   ```

2. Install the Wails CLI (once):

   ```powershell
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```

3. Build the frontend and the application:

   ```powershell
   cd frontend
   npm ci
   npm run build
   cd ..
   wails build
   ```

   The executable is written to `build/bin/Jarvis.exe`. A plain `go build` also works once `frontend/dist` exists.

4. Run `build/bin/Jarvis.exe`. On first launch, JARVIS checks Ollama, recommends a model based on available RAM, and offers to pull it. Settings, chat history, notes, and logs are stored in `%AppData%\Jarvis`.

## Voice engine

Speech recognition stays local. Large binaries and models are **not** embedded in `Jarvis.exe`; they live next to it in `build/bin/voice-engine` (or `%AppData%\Jarvis\voice-engine`):

```
voice-engine/
  libvosk.dll, libgcc_s_seh-1.dll, libstdc++-6.dll, libwinpthread-1.dll
  vosk-model-small-ru-0.22/
  vosk-model-small-en-us-0.15/        (optional, English wake words)
  whisper/
    whisper-cli.exe, ggml.dll, ggml-cuda.dll, cublas64_12.dll, ...
    ggml-large-v3-turbo.bin
```

The app searches `voice-engine` next to the executable, then `%AppData%\Jarvis\voice-engine`. The engine status and the detected Whisper model are shown in Settings. The full `ggml-large-v3-turbo.bin` model is preferred; q8/q5/large-v3/medium/small fallbacks are accepted. If Whisper is missing, wake-word detection still loads but commands cannot be transcribed. The English Vosk model is optional: without it, English commands are still recognized by Whisper, but only the Russian wake word is detected.

JARVIS does not speak its replies. Answers are shown in the window only; short confirmation phrases and the greeting still use the local voice pack. Wake words are detected with Vosk; commands and free-form chat are transcribed with Whisper and sent only to the local Ollama server. Weather, first-launch geolocation, and optional web search use the network. Every outbound web request is resolved and checked against a public-address allowlist first: private, loopback, link-local, and other non-public destinations are rejected. The headless browser additionally sends all traffic through a loopback filtering proxy.

To keep JARVIS available at every logon, enable **Auto-start with Windows** in Settings. The entry runs `Jarvis.exe --hidden`: the full application starts with its window hidden in the tray, so the wake word, the `Ctrl+Shift+J` hotkey and the tray icon work immediately. Legacy `--voice-agent-supervisor` and `--voice-agent` entries are migrated to `--hidden` on the next start. The Settings panel shows the current Run value, whether the executable exists, and a **TEST START** button that launches a headless self-check (`--autostart-check`) without disturbing the running instance. JARVIS also clears a stale Explorer `StartupApproved` flag, which is a common reason a valid Run entry silently stops starting.

### Voice engine integrity (optional)

`checksums.json` in the engine root enables SHA-256 verification of the core executables and DLLs:

```powershell
go run ./tools/checksumgen -root build\bin\voice-engine
go run ./tools/checksumgen -root build\bin\voice-engine -verify
```

The default manifest covers `libvosk.dll`, `whisper/whisper-cli.exe`, `whisper/whisper.dll`, and `whisper/ggml.dll`. Add `-all` to hash every file under the engine root, including the Whisper models; this makes startup verification noticeably slower. A missing manifest is accepted for backward compatibility. Set `JARVIS_VOICE_REQUIRE_CHECKSUMS=1` to make it mandatory. If a listed file is missing or does not match, the UI reports the failure and voice input stays disabled; the background agent re-checks a failing engine once a minute so it can be repaired without a restart.

## Development

```powershell
wails dev
```

This starts the Vite dev server with hot reload and exposes Go bindings to the browser dev server. To type-check and build the frontend only:

```powershell
cd frontend
npm run build
```

## Commands

Commands are recognized only when the message starts with `/`. A bare word such as `smart search` is sent to the model.

| Command | Description |
| --- | --- |
| `/help` | List available commands. |
| `/time` | Current date and time. |
| `/clear` | Clear the chat history. |
| `/system` | Basic system information. |
| `/screenshot`, `/screen` | Capture the screen after confirmation. Use the screen-analysis button in the UI to ask a vision model about the screenshot. |
| `/open <app>` | Launch a detected application after confirmation. |
| `/apps` | List detected applications from the local catalog. |
| `/volume <0-100>`, `/volume mute`, `/volume unmute` | Control the default output device. |
| `/shutdown`, `/restart` | System power actions after a `YES` confirmation. |
| `/ls [path]`, `/cat <path>` | List or read files in allowed folders. |
| `/mkdir <path>`, `/touch <path>`, `/write <path> <text>` | Create directories or files in allowed folders. |
| `/note <text>`, `/notes` | Save and list notes. |
| `/audit` | Show recent audit log entries. |
| `/remind <minutes> <text>` | Set a reminder. Minutes may be 1 to 10080 (7 days). |
| `/timer <minutes>` | Set a timer. |
| `/reminders` | List pending reminders. |
| `/game [status\|list\|add [name]\|forget <name>]` | Show the current game session, list remembered games, remember the foreground game, or forget one. |
| `/smart <query>` | Web search plus a model summary. Requires web search to be enabled and a `YES` confirmation. |
| `/search <query>` | DuckDuckGo results after a `YES` confirmation. Requires web search to be enabled. |
| `/imagine <prompt> [--size WxH]` | Generate an image online. Requires the image-generation opt-in in Settings. |

Slash commands with side effects ask for confirmation. For `/shutdown`, `/restart`, `/screenshot`, `/open`, `/search`, and `/smart`, reply `YES` (or `Y`); any other message cancels the pending action. Pending confirmations expire after two minutes.

## Configuration and data

All data stays under `%AppData%\Jarvis`:

- `settings.json` — DPAPI-encrypted settings (Ollama URL, model, system prompt, web search and voice toggles, auto-start preference, image-generation opt-in, autonomy level, briefing and game toggles).
- `history.json` — DPAPI-encrypted chat history, limited to the last 40 user/assistant messages. The system prompt is not stored here; it is read from settings on startup. Legacy plaintext files are still readable and are migrated on the next save.
- `history.json.bak` — previous valid encrypted history file, used automatically if `history.json` is missing or corrupt.
- `notes.json` — DPAPI-encrypted notes. Legacy plaintext files remain readable and are migrated on the next save.
- `reminders.json` — pending reminders and timers. A corrupt file is moved to a timestamped backup and an empty list is used.
- `games.json` — remembered games and the last 30 days of per-game playtime. Only executable names, display names, and paths are stored.
- `autostart-check.txt` — timestamp written by the **TEST START** self-check.
- `*.corrupt-<timestamp>` — recoverable copy created before a corrupt settings or history file is replaced. The UI reports the backup path.
- `voice\` — phrase WAV files copied from the `voice` folder on first launch. The bundled folder is read from next to the executable (or the project root during development). The repository does not include the original sound pack for copyright reasons; add your own licensed WAVs to `voice\` before building. Replace a file in the AppData copy to customize a phrase; existing files are never overwritten by the app. Missing files are skipped silently.
- `voice-engine\` — optional per-user copy of the local speech engines and models. The portable `build/bin/voice-engine` folder is preferred when present.
- `voice-ipc.json` — DPAPI-protected endpoint and token for the authenticated loopback command channel. It is removed when the app exits.
- `voice-agent.log` — background agent metadata and error log. Raw transcripts are redacted unless `JARVIS_VOICE_DEBUG=1` is set. It rotates to `voice-agent.log.1` at 1 MiB.
- `logs\YYYY-MM-DD.log` — daily audit log (action names and web-search queries; chat message contents and command arguments are not logged). Entries older than 30 days are removed at startup.

Settings can be changed in the in-app Settings panel. The Ollama URL must be an `http` or `https` URL; connection failures are reported in the UI. Model names, the system prompt, city, wake word, microphone fields, and weather coordinates are validated before saving. Chat history is restored when the app starts; `/clear` removes both the in-memory and persisted conversation. JARVIS does not read its answers aloud: replies appear in the chat window only, while short confirmation phrases and the greeting still play from the local voice pack. The recognition language can be left on `Auto` or pinned to Russian/English. Stopping voice input also cancels an in-flight spoken request instead of waiting for the model timeout. The microphone check in Settings reports device count, Windows privacy consent, endpoint state, and a short peak-level measurement.

**Proactivity and autonomy.** The autonomy level controls how much JARVIS does without asking: `off` disables all unprompted messages, `suggestions` (default) offers them, and `trusted` also skips the `/open` confirmation. Dangerous actions (shutdown, restart, screenshots, file writes) always require confirmation. Startup briefing, game detection, and break reminders each have their own toggle. Unprompted messages are never delivered during quiet hours (23:00–08:00) or while the window is hidden; the briefing is delivered the first time the window becomes visible. The built-in persona is versioned: upgrading JARVIS replaces an untouched default prompt but preserves a prompt you edited.

## Security model

- **Explicit confirmation** for screenshots, `/open` commands, web searches, and system power actions. Confirmation state is a typed intent with a two-minute TTL, not a substring match on model output. Clicking an app in the sidebar launches it directly, without a second prompt.
- **Path allowlist** for file operations, with traversal and symlink checks. File writes never overwrite existing content (`/write` is create-only; `/touch` refuses existing files).
- **Prompt-injection sanitation** for fetched web content and voice input. Untrusted page text is truncated and stripped of instruction-like patterns before it reaches the model; the same sanitizer neutralizes recognized voice commands before they are stored or routed. This reduces risk; it does not eliminate it.
- **Encrypted settings, history, and notes** using Windows DPAPI, scoped to the current user. Legacy plaintext history/notes are readable for migration but are re-encrypted on the next save.
- **Authenticated background-agent IPC.** The agent launches the app without command-line payloads and delivers commands over a random loopback port. Requests are HMAC-SHA256 signed with a DPAPI-protected token, checked against a 30-second clock window, and deduplicated by nonce.
- **Browser egress filtering.** Chromium uses a loopback proxy that resolves each destination and rejects private, loopback, link-local, carrier-grade NAT, documentation, and other non-public addresses, including DNS-rebinding and redirect targets. Only ports 80 and 443 are allowed. Direct web requests (`/search`, the `/smart` fallback, and image generation) use the same public-IP validation in their HTTP transport, so a redirect cannot reach a local service.
- **Image generation is opt-in.** Prompts are sent to Pollinations.ai only after the user enables image generation in Settings.
- **Optional voice-engine checksums.** A generated manifest detects modified or missing core speech binaries before the microphone is used.
- **Audit log** of actions and confirmations, retained for 30 days.
- **No telemetry.** JARVIS does not send analytics or usage data anywhere.
- **Network boundaries:** Ollama listens locally by default. Weather and first-launch geolocation use Open-Meteo; web search sends the query to DuckDuckGo; image generation sends the prompt to Pollinations.ai only when opted in. These features are optional and clearly labeled in the UI.

### Known limitations

- Windows only.
- Voice input requires the external `voice-engine` assets and a working microphone. Recognition quality depends on the microphone, room noise, and the Whisper model.
- Vosk wake-word detection is optimized for the default «привет Джарвис» phrase. A custom wake word can be entered in Settings, but recognition quality is not guaranteed for arbitrary phrases.
- Streaming applies to free-form chat only; slash commands and screen analysis return complete responses.
- Screen analysis requires a vision-capable model; text-only models cannot describe screenshots.
- Weather, web search, and image generation depend on third-party services that may rate-limit or change. Image generation must be enabled in Settings first.
- The global hotkey and the CoreAudio volume path are implemented but are not covered by automated tests; they need a real desktop session to verify.
- Game detection is heuristic: a remembered executable is always recognized, while unknown games are inferred from a fullscreen or caption-less foreground window. Games that run windowed with a normal title bar are not detected until you run `/game add`. Detection never reads game memory or injects code, so it cannot know the in-game state.
- Proactive messages and reminders fire only while JARVIS is running. They are queued in memory, not delivered by a separate service, and the startup briefing waits until the window is visible.
- No signed installer or automatic updater is provided. `build/windows/installer/project.nsi` contains unsigned NSIS scaffolding. Copy `voice-engine` next to `Jarvis.exe` when deploying, or install it under `%AppData%\Jarvis\voice-engine`. Building the installer with `makensis -DBUNDLE_VOICE_ENGINE project.nsi` embeds the staged `build\bin\voice-engine` folder instead (the resulting installer is several gigabytes when models are included).
- **Voice-engine checksums** only cover files listed in `checksums.json`; models are excluded unless the manifest is generated with `-all`.

## Project structure

```
app.go                  Wails-bound application API and routing
app_voice.go            Voice lifecycle, Russian/English actions, weather, second-instance commands
proactive.go            Startup briefing, reminder delivery, quiet hours, model watchdog
game.go                 Game session integration, /game command, break prompts
voice_agent.go          Supervised background wake-word agent and supervisor (legacy mode)
main.go                 Wails entry point (--hidden, --voice-agent, --voice-agent-supervisor, --voice-wake, --autostart-check)
internal/applock        Foreground-app, background-agent, and supervisor mutexes
internal/appscan        Application catalog: Start Menu, UWP, App Paths, Program Files
internal/audit          Local audit log with 30-day retention
internal/audio          Windows CoreAudio volume control
internal/autostart      HKCU Run registration, StartupApproved repair, legacy-mode migration
internal/browser        Headless page fetching and egress-filtering proxy
internal/commands       Slash commands, allowlisted file operations, encrypted notes
internal/config         Encrypted settings, corrupt-file backup, auto-start/image/proactivity preferences
internal/dpapi          DPAPI wrapper
internal/gamewatch      Read-only foreground game detection, playtime, Game DVR merge
internal/hotkey         Global hotkey registration
internal/intent         Typed confirmation intents with TTL
internal/ipc            Authenticated loopback command channel for the background agent
internal/jobobject      Job object for child process cleanup
internal/netguard       Public-address validation for direct HTTP requests
internal/ollama         Ollama HTTP client
internal/reminders      Persistent reminders and timers
internal/safepath       Path allowlist and traversal protection
internal/sanitize       Untrusted web content sanitization
internal/setup          Hardware detection and model recommendation
internal/tray           System tray icon and menu
internal/voice          Phrase table and Windows PlaySound player
internal/voiceengine    External engine/model path resolution and checksum verification
internal/voiceinput     Microphone capture, VAD, Vosk wake word, Whisper transcription
internal/weather        Open-Meteo forecast and geolocation
internal/winwindow      Win32 window visibility check used for close-to-tray state
tools/checksumgen       Voice-engine checksum manifest generator and verifier
tools/ipcsend           Diagnostic command sender for the authenticated IPC channel
voice/                  Place your own licensed phrase WAV files here (not bundled)
build/bin/voice-engine  Local Vosk/Whisper binaries and models (not embedded)
frontend/               Vue 3 + Vite UI
website/                Static landing page
```

## Tests and checks

```powershell
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
cd frontend
npm ci
npm run build
```

`npm run build` runs `vue-tsc --noEmit` before Vite, so TypeScript errors fail the build. `go test ./...` covers commands (including large-directory caps), the application catalog, encrypted config/history/notes with corrupt-file recovery, persona migration and autonomy normalization, Russian and English intents (including reminders, game intents, full URLs and search-style weather phrases), reminder persistence and corrupt-file recovery, path safety, sanitization, the Ollama client, setup recommendations, audit logging and retention, app routing, weather parsing and response-size limits, stored-coordinate forecasts, voice-history sanitization, transcript hallucination filtering, microphone diagnostics, browser URL safety and egress-proxy rejection, direct-request public-address validation, authenticated IPC delivery and tamper rejection, app/agent mutex behavior, voice-engine checksum verification, and voice phrase/assets handling. Run `go test -race ./...` for the concurrency check.

Hardware and network checks are opt-in with the `manual` build tag, for example:

```powershell
$env:JARVIS_VOICE_ENGINE = "C:\path\to\voice-engine"
$env:JARVIS_TEST_WAV = "C:\path\to\speech.wav"
# Optional: select a microphone by ID (list them with TestManualDevices).
$env:JARVIS_TEST_DEVICE = "device-id"
# Optional: record a short sample instead of measuring levels.
$env:JARVIS_TEST_RECORD = "C:\path\to\record.wav"
go test ./internal/voiceinput -tags manual -run Manual -v
go test ./internal/weather -tags manual -run Manual -v
```

## License

MIT. See [LICENSE](LICENSE).
