# AGENTS.md

Windows-only Wails 2 desktop assistant: Go backend, Vue 3 frontend, local Ollama model. Not a web app, not cross-platform.

## Build order (matters)

`main.go` embeds `frontend/dist` with `//go:embed all:frontend/dist`, so the root Go package will not compile on a fresh clone until the frontend is built:

```powershell
cd frontend
npm ci
npm run build
cd ..
wails build -ldflags "-linkmode=internal"   # -> build/bin/Jarvis.exe
```

- `npm run build` runs `vue-tsc --noEmit` first; TypeScript errors fail the build.
- `wails dev` runs Vite + Go with hot reload.
- A running `build/bin/Jarvis.exe` locks the output file and the single-instance lock; stop it before rebuilding.
- Always pass `-ldflags "-linkmode=internal"`. Without it the MinGW external linker prints `.rsrc merge failure: multiple non-default manifests`; the build still succeeds, but the warning hides real failures and CI logs get noisy. With the internal linker the warning disappears and the executable starts normally. Release builds should extend the same flag: `-ldflags "-linkmode=internal -X main.version=1.2.3"`.
- Go 1.26.x only. Go 1.27 currently breaks the Wails build. Wails v2.13 matches `go.mod`; do not bump either without a full build.
- On this workstation `go`, `node`, and `wails` may be missing from `PATH`. Working copies live under `%TEMP%\opencode\*toolchain*\`; GitHub Desktop supplies `git.exe` under `%LOCALAPPDATA%\GitHubDesktop\app-*\resources\app\git\cmd`.

## Verify

```powershell
gofmt -l .               # must be empty
go vet ./...
go test ./...
go test -race ./...
```

CI (`.github/workflows/ci.yml`) runs frontend build, then `gofmt`, `vet`, `test`, `test -race`, `build` — in that order, on Windows.

Focused run: `go test ./internal/netguard -run TestIsPublicIPRejectsSpecialRanges -v`

Binding drift check: `go test . -run TestFrontendDeclaresEveryBoundMethod -v`

Manual/hardware/network tests use the `manual` build tag and are excluded from CI: `internal/audio`, `internal/netguard`, `internal/voiceinput`, `internal/weather`. They need a real device, microphone, or network and env vars such as `JARVIS_TEST_WAV`, `JARVIS_VOICE_ENGINE`.

## Repository traps

- `./jarvis/` is a stale, gitignored copy of the project with its own `go.mod`. Never edit it. `gofmt -l .`, grep, and file searches will show duplicate sources from it; scope checks to the real tree. CI does not see this directory.
- `frontend/wailsjs/` is generated and gitignored. `Window.go.main.App` is hand-declared in `frontend/src/App.vue`; whenever you add an exported `*App` method, add its TypeScript signature there too or the UI cannot call it. `bindings_test.go` reflects over `*App` and fails when a signature is missing, but it cannot check argument types.
- `voice/*.wav` is copyrighted and must never be committed. Tests must pass without the local sound pack.
- `build/bin/voice-engine` (~3.4 GB) is not in git. Engine layout and resolution order live in `internal/voiceengine`. `checksums.json` has two sections: `files` (17 startup binaries, hashed by `Verify()`) and `on_demand` (models and large runtimes ≥ 64 MB, hashed only by `VerifyOnDemand()`). Regenerate with `go run ./tools/checksumgen -root build\bin\voice-engine` after replacing engine files.
- `LaunchApp` is deliberately restricted to the curated `DetectApps()` catalog used by the sidebar. Do not add a raw path/name launch path back; the path is re-validated as absolute, existing, and a file, and denied launches are audit-logged.
- Version lives in `version.go` (`var version = "0.1.0"`, overridable with `-X main.version=...`) and is exposed through `App.GetVersion()` and `StatusInfo.Version`. Keep the default in sync with the `info` block in `wails.json`.
- Piper binaries in the engine folder are unused — the app has no TTS integration.
- `*.exe`, `*.dll`, `*.bin`, `build/bin/`, `frontend/dist/` are gitignored. Never commit build artifacts.
- `app.go` (~1900 lines) and `frontend/src/App.vue` (~1974 lines) are monoliths with no component split. Expect large diffs.
- User data in `%AppData%\Jarvis` is DPAPI-encrypted and not portable between users or machines; a copied `settings.json` will fail to decrypt elsewhere.

## Architecture

- `main.go` handles CLI modes before Wails starts: `--hidden`, `--voice-agent`, `--voice-agent-supervisor`, `--voice-wake`, `--autostart-check`. A new flag must be added there, and modes must return before `wails.Run`.
- Wails-bound API lives on `*App`: `app.go` (core API, routing, startup/shutdown), `app_voice.go`, `setup.go` (Ollama wizard), `proactive.go`, `game.go`. The frontend calls `window.go.main.App.*` and listens with `runtime.EventsOn`.
- Voice runs in the main app and in a separate `--voice-agent` process that calls `voiceengine.Resolve()` independently. Replacing engine files requires stopping that process because it locks DLLs.
- `internal/commands` = slash commands and file operations; `internal/intent` = typed confirmations with TTL; `internal/ollama` = model client; `internal/voiceinput` = mic/VAD/Vosk/Whisper.
- `website/index.html` is a standalone static landing page, unrelated to the app build.

## Security invariants (do not regress)

- All outbound HTTP must go through `internal/netguard` (`NewClient`, `DialContext`, `IsPublicURL`). Never use `http.Get` or a bare `&http.Client{}`. Only ports 80/443, public IPs only, redirects re-validated. The headless browser additionally routes Chromium through the loopback egress proxy in `internal/browser`. Installer/model downloads follow the same rule and additionally reject non-HTTPS redirects and more than five hops.
- File operations must use `internal/safepath` (allowlist: Desktop, Documents, JARVIS config dir). Writes are create-only.
- Untrusted web and voice text must pass `internal/sanitize` before reaching the model or command router.
- Side-effect actions use typed confirmations in `internal/intent` (2-minute TTL), never substring matching on model output.
- Voice commands reach the app over HMAC-authenticated loopback IPC, never through process argv.
- Never run `/shutdown` or `/restart` on a development machine, and do not launch the app to test them.

## Release state

- Repo: `bbogolubov2010-netizen/jarvis`, branch `main`, public.
- Version `0.1.0`. Releases should stamp `-X main.version=<tag>`; the UI shows `StatusInfo.Version`.
- No signed installer and no updater yet. `build/windows/installer/project.nsi` is unsigned NSIS scaffolding, intended for a per-user install under `%LOCALAPPDATA%\Programs\JARVIS`; `wails.json` now carries the `info` block (company/product/version/copyright).
- Tests and CI must never require the local voice pack or the 3.4 GB engine.
