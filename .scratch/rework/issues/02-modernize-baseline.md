# 02 - Modernisierungs-Baseline

Status: resolved
Type: task

## Ziel

Go-Toolchain, Dependencies und Idiome auf aktuellen Stand bringen, ohne Verhalten zu ändern. Basis für alle Folgetickets.

## Aufgaben

- [x] `go.mod`: `go 1.27.x` (aktuelles Stable: 1.27.4)
- [x] Deps latest: `paho.mqtt.golang`, `testify`; `zap` entfernen → `slog` (stdlib)
- [x] Logging migrieren: paho-Log-Rewiring (`log.go`), alle `zap.L()`-Stellen; dabei die nie formatierten Printf-Literale (`main.go:41`, `config.go:70`) korrigieren
- [x] `ioutil` → `os` (`internal/mqtt/config/topic_test.go`)
- [x] `errors.Is` statt `== context.Canceled` (`trigger.go:161`)
- [x] `time.Tick` → `time.NewTicker` mit `Stop()` (`sensor.go:39`)
- [x] `panic` bei `json.Marshal` in `hassio/client.go` durch Fehlerbehandlung ersetzen
- [x] CI: Workflows auf aktuelle Actions (`actions/checkout@v4/5`, `actions/setup-go@v5`, Go 1.27.x), deprecated Release-Actions ersetzen (Upload via `softprops/action-gh-release` oder `gh release`)
- [x] Build-Matrix (linux+windows × 386/amd64/arm/arm64) unverändert lassen

## Akzeptanzkriterien

- `go build ./...` und `go vet ./...` grün mit Go 1.27.x
- `go test ./...` grün (bestehender topic_test unverändert grün)
- CI-Workflow pinnt Go 1.27.x, keine deprecated Actions mehr
- Verhalten ändert sich nicht (keine funktionalen Änderungen)

## Abhängigkeiten

Keine. Nicht blockiert durch 01 (Fork).
