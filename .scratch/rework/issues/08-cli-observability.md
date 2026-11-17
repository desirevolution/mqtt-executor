# 08 - CLI & Observability-Basics (A4 --version, A5 Log-Format)

Status: resolved
Type: task

## Ziel

Standard-CLI-Basics: `--version` und wählbares Log-Format.

## Aufgaben

- [x] Flag `--version`: `versionString()` aus `runtime/debug.BuildInfo` (Modul-Version + VCS-Revision, gekürzt auf 7 Zeichen); `--version` short-circuited **vor** der Config-Validierung (kein `-device-id` nötig)
- [x] Flag `-log-format=text|json` (Default `text`): `setupLogging(format, out) error` in `log.go` (TextHandler/JSONHandler + paho-Rewiring); `LoadConfig` ruft es nach `flag.Parse`, invalides Format ⇒ fatal
- [x] Logging-Setup refactorisiert: `init()` setzt nur den Text-Default (für Flags/Startup-Fehler)
- [x] Tests: `TestSetupLoggingInvalidFormat`, `TestSetupLoggingTextAndJson` (JSON-Zeile parsebar, Global-State via `t.Cleanup` auf `os.Stderr`/text zurückgesetzt), `TestVersionString`

## Akzeptanzkriterien

- [x] `mqtt-executor --version` druckt Version (Smoke: `v0.0.0-…ac41a5a+dirty (ac41a5a)`)
- [x] `-log-format=json` erzeugt parsebares JSON pro Zeile
- [x] Default bleibt text
- [x] `go test ./...` grün (63/63)

## Abhängigkeiten

02 (slog-Baseline).
