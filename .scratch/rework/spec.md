# Spec: Fork-Rework mqtt-executor

Status: open

## Ziel

Den Fork von `rainu/mqtt-executor` modernisieren und erweitern: Transport-Layer abstrahieren, einen synchronen Webhook-Ingress neben MQTT einführen, MQTT optional machen, Config auf YAML umstellen, Go + Dependencies + CI auf aktuellen Stand bringen.

## Hintergrund

- Aktuell: Go 1.14, paho.mqtt.golang 1.4.2, zap 1.23.0; paho-Client hart in alle Komponenten verdrahtet; striktes `encoding/json` (die json5-Beispiele im README sind invalid); CI auf deprecated Actions mit Go 1.14; genau ein Testfile.
- Befunde aus der Architektur-Kartierung (2026-11-17): Trigger-Race (`isCommandRunning`/`registerCommand` nicht atomar, `internal/mqtt/trigger.go:112-130`), `time.Tick`-Leak (`sensor.go:39`), `execErr == context.Canceled` statt `errors.Is` (`trigger.go:161`), fehlende `token.Wait()` beim Publish (`hassio/client.go`), `panic` beim Marshal (`hassio/client.go:143,166,190,215`), zap mit nie formatierten Printf-Literalen (`main.go:41`, `config.go:70`).

## Entscheidungen (Grilling, bestätigt 2026-11-17)

| # | Entscheidung | Wahl |
|---|---|---|
| 1 | Repo-Strategie | Echter GitHub-Fork, breaking changes erlaubt |
| 2 | Name / Modul-Pfad | `mqtt-executor` behalten, Modul-Pfad bleibt `github.com/rainu/mqtt-executor` |
| 3 | Webhook-Rolle | Synchron starten **und** abfragen, jeweils one way (kein Push-Output) |
| 4 | Update-Strategie | Volle Modernisierung: Go 1.27.x, alle Deps latest, Idiome migrieren, CI aktualisieren |
| 5 | MQTT | Optional — Broker-URL leer ⇒ reiner Webhook-Betrieb, HA-Discovery automatisch aus |
| 6 | Seiteneffekte | Geteilt: Webhook-Trigger publishen STATE/RESULT nach MQTT (wenn Broker da); GET-Sensor-Abfrage publishet den Wert ebenfalls |
| 7 | Webhook-Security | Keine Auth (Vertrauensfrage beim Betreiber); Bind-Flag Default `:8080`, Log-Warnung bei Bind auf alle Interfaces |
| 8 | Config-Format | YAML (`yaml.v3`), 1:1-Spiegel der bisherigen Struktur; JSON wird nicht mehr gelesen |
| 9 | Docker | Ja: Multi-Stage-Dockerfile (scratch, CGO off) + Multi-Arch-OCI im Release-Workflow |
| 10 | Test-Niveau | Neue Kernpfade: Transport-Interface, Webhook-Handler, Config-Validation, Race-Regression |
| 11 | Logging | `zap` → `slog` (stdlib) |
| 12 | Sensoren ohne Broker | Fail fast: `sensor`/`multi_sensor` ohne Broker ⇒ Config-Validierungsfehler |
| 13 | A1 Strict-Parsing | Unbekannte Config-Keys ⇒ Startfehler (`KnownFields(true)`) — fängt Tippfehler |
| 14 | A2 Command-Timeout | Optionales `timeout` pro Trigger/Sensor (Default: keiner, wie bisher) |
| 15 | A3 LookPath-Check | Command-Existenz beim Start prüfen, sonst Fail fast |
| 16 | A4 `--version` | Flag gibt Version aus (`runtime/debug.BuildInfo`) |
| 17 | A5 Log-Format | `-log-format=text\|json`, Default `text` |
| 18 | B1 Payload→stdin | Opt-in pro Trigger (`stdin_payload: true`); gilt für MQTT- **und** Webhook-Trigger (geteilte Semantik) |
| 19 | B2 Health-Endpoint | `GET /status` am Webhook-Server (Status + Uptime) |
| 20 | B3 Metrics | `/metrics` via prometheus `client_golang` (neue direkte Abhängigkeit) |
| 21 | B4 Publish-on-change | Option pro (Multi-)Sensor `publish_on_change: true`, Default `false` |
| 22 | B5 Hot-Reload | SIGHUP liest Config neu: Trigger-Resubscribe, Ticker-Neustart |
| 23 | API-Doku-UI | Handgeschriebene OpenAPI-3-Spec (`/openapi.yaml`) + eigene, eingebettete Playground-UI (`/docs`, go:embed, offline, keine Dependencies); immer an, wenn der Webserver läuft |
| 24 | Docker-Basis | **alpine** (revidiert von `scratch` nach empirischem Befund: scratch hat kein Userland ⇒ keine Commands ausführbar); HEALTHCHECK via `wget` gegen `/status`; drei dokumentierte Einbindungsmuster (Extended Image / Volume-Mounts / `COPY --from`-Binary) |
| 25 | Image-Tags | Nur Release-Tags (`v*`) bauen das Image (`latest` + Semver) |
| — | A6 MQTT-TLS-Flags | **nicht gewählt** (explizit abgelehnt, 2026-11-17) |

## Architektur

### Transport-Interface

Ersetzt den konkreten paho-`MQTT.Client`-Feld-Typ überall (`main.go`, `trigger.go`, `sensor.go`, `status.go`, `hassio/client.go`):

```go
type Publisher interface {
    Publish(topic string, qos byte, retained bool, payload any) error
}
// Subscribe wird nur für MQTT-Trigger-Ingress gebraucht; HTTP braucht es nicht.
```

- MQTT-Implementierung: Wrapper um paho (inkl. `token.Wait()`-Handling).
- Webhook-Server (`net/http`, Go-1.22+-Routing) ist ein **Ingress-/Abfragekanal**, kein zweites Transport-Interface: HTTP fragt synchron, Antworten gehen in die HTTP-Response; MQTT-Seiteneffekte laufen über denselben `Publisher` (No-op ohne Broker).

### Webhook-Endpoints

| Route | Semantik | Response |
|---|---|---|
| `POST /trigger/{name}` (Body = Payload) | Command starten, synchron auf Ende warten | `{"state": "STOPPED", "result": "..."}` |
| `DELETE /trigger/{name}` (Alias: `POST …?action=stop`) | laufenden Run abbrechen (idempotent) | `{"state": "STOPPED"}` |
| `GET /sensor/{name}` | Sensor-Command ausführen, Output zurück (+ Publish nach MQTT, wenn Broker) | Output |
| `GET /status` | Health/Readiness (Status + Uptime) | `{"status": "ok", "uptime": "..."}` |
| `GET /metrics` | Prometheus-Metriken (Runs, Fehler, Durations) | exposition format |

- Flag `-web` (Default `:8080`, `""` deaktiviert den Server).
- Trigger ohne Broker: STATE/RESULT nur in der HTTP-Antwort, kein Publish.
- Trigger mit `stdin_payload: true`: Body bzw. MQTT-Payload wird dem Command als stdin übergeben.

### Erweiterte Semantik

- **Command-Timeout** (`timeout`, optional pro Trigger/Sensor): der Aufrufer wrappt den Execution-Context mit `context.WithTimeout` — der Executor bleibt generisch.
- **Publish-on-change** (`publish_on_change: true` pro (Multi-)Sensor): Publish nur, wenn der Command-Output sich gegenüber dem letzten Run unterscheidet.
- **Hot-Reload** (SIGHUP): Worker (Trigger/Sensor/Status) sauber stoppen, Config neu laden, Worker neu starten; HA-Discovery wird neu published.

### Betriebsmodi

| | Mit Broker | Ohne Broker |
|---|---|---|
| Trigger (MQTT-Ingress) | ✓ | — |
| Trigger (Webhook) | ✓ (+ Publish STATE/RESULT) | ✓ (nur HTTP-Response) |
| Intervall-Sensoren | ✓ | ✗ Validierungsfehler (fail fast) |
| Sensor per `GET` | ✓ (+ Publish) | ✓ (nur HTTP-Response) |
| HA-Discovery | ✓ | automatisch aus |

## Nicht-Ziele

- Keine Auth am Webhook (explizit entschieden).
- Kein goreleaser / kein neues Release-Tooling — Workflows werden in-place aktualisiert.
- Kein Umbenennen von Projekt, Modul-Pfad oder Binary.
- Kein Go-seitiges Templating — `{{...}}` bleibt HA-`value_template`.
- Keine Upstream-PRs (vorerst); Fork dient als Ablagepunkt.

## Tickets

| # | Ticket | Hängt ab von |
|---|---|---|
| 01 | `01-fork-setup.md` — gh installieren/auth, Fork anlegen, Remote | — (parallel möglich) |
| 02 | `02-modernize-baseline.md` — Go 1.27, Deps, slog, Idiome, CI-Workflows | — |
| 03 | `03-transport-interface.md` — Publisher-Interface, MQTT-Wrapper, Race-Fix | 02 |
| 04 | `04-config-yaml.md` — YAML-Config, Strict-Parsing (A1), portierter Validationstest | 02 |
| 05 | `05-mqtt-optional.md` — Broker optional, Validierung Sensoren, Discovery-Abschaltung | 03, 04 |
| 06 | `06-webhook-server.md` — HTTP-Server, Trigger/Sensor/Health-Endpoints, Payload→stdin (B1), Tests | 03, 04, 05 |
| 07 | `07-command-hardening.md` — Command-Timeout (A2), LookPath-Check (A3) | 04 |
| 08 | `08-cli-observability.md` — `--version` (A4), `-log-format` (A5) | 02 |
| 09 | `09-metrics.md` — Prometheus `/metrics` (B3) | 06 |
| 10 | `10-sensor-on-change.md` — publish-on-change (B4) | 04, 05 |
| 11 | `11-hot-reload.md` — SIGHUP-Reload (B5) | 06, 07, 10 |
| 12 | `12-docker-oci.md` — Dockerfile (scratch) + Multi-Arch-Release, nutzt `/status` | 06 |
| 13 | `13-readme-docs.md` — README (YAML, Webhook, Docker, alle Features), startup/ prüfen | alle |
| 14 | `14-api-docs.md` — OpenAPI-Spec + eingebettete Playground-UI (A23) | 06 |

## Risiken

- paho.mqtt.golang latest kann API-Änderungen haben (Token-Typen, Optionen) — betrifft den Wrapper in 03.
- Fork-Setup braucht `gh`-Auth (Device-Flow, User-Interaktion) oder manuelles Forken im Browser.
- Multi-Arch-Docker-Builds (arm/v7, 386) können Build-Time kosten; Binaries sind der primäre Artefakt-Pfad.
- Verhalten von HA-Entities darf sich nicht ändern (Payload-Struktur der Discovery bleibt unverändert).
