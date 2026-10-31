# JARVIS voice phrases

JARVIS plays short confirmation phrases and a greeting from WAV files in this
folder. The original sound pack is not included in this repository for
copyright reasons.

To enable spoken phrases, place your own licensed WAV files here using the
stable ASCII names referenced by `internal/voice` (for example `greeting.wav`,
`yes.wav`, `error.wav`, `request_done.wav`). At first launch JARVIS copies the
files it finds into:

```
%APPDATA%\Jarvis\voice
```

The config-directory copy takes precedence, so you can replace any phrase
there without changing the application. The source folder next to the
executable (or this folder during development) is used as a fallback.

Missing files are skipped silently: text replies and all commands continue to
work without the sound pack.

Do not redistribute third-party film or character audio unless you have the
necessary rights.
