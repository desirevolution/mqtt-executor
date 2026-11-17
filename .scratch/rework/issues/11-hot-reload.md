# 11 - Config-Hot-Reload (B5)

Status: resolved
Type: task

## Ziel

Neustartfreies Einlesen der Config per SIGHUP: Worker sauber stoppen, Config neu laden, Worker neu starten.

## Aufgaben

- [x] Signalkanal um SIGHUP erweitert (eigener Kanal, Select-Loop in main); Reload-Semantik: Trigger-/Sensor-Worker `Close()`, Config neu laden, Worker via `applyWorkerConfig()` neu `Initialise()`
- [x] Executor + Webhook-Server + MQTT-Verbindung + **StatusWorker laufen weiter** (Entscheidung: Availability bleibt von der Startup-Config — ein Reload published sonst fälschlich „Unavailable" an HA)
- [x] Fehler beim Reload (Datei fehlt, invalide Config, LookPath, Broker-Regel): **alter Zustand bleibt aktiv**, Fehler ins Log — `loadReloadConfig()` validiert alles, BEVOR irgendetwas abgebaut wird
- [x] HA-Discovery nach Reload neu gepublisht (im Broker-Modus)
- [x] Struktur: `applyWorkerConfig()` (Start + Reload) und `loadReloadConfig()` (geteilte Validierung, `LoadConfig` nutzt sie auch) in main/config
- [x] **Race-Sicherheit für den Reload**: `Trigger`-Lookup-Maps werden unter Write-Lock atomar getauscht (`Initialise`), Reads (`StartSync`/`Exists`/`ReInitialise`/`Close`) locken/kopieren; `SensorWorker.Queryables` → privat + `SetQueryables` (RWMutex)
- [x] Laufende Trigger-Commands zum Reload-Zeitpunkt dürfen auslaufen (documented edge: kurze Zeit möglicher Doppel-Start möglich)
- [x] Tests: `TestInitialiseSwapsConfiguration` (Swap + Concurrency-Stress), `TestSetQueryables`, `TestLoadReloadConfig` (gültig/invalides Command/Sensor ohne Broker)
- [x] E2E-Smoke: HUP mit geänderter Trigger-Liste (v1→404, v2→200), HUP mit invalider Config ⇒ No-op + Log, alter Trigger bleibt aktiv

## Akzeptanzkriterien

- [x] `kill -HUP <pid>` wendet die neue Config an, ohne den Prozess zu beenden
- [x] Invalide Config beim Reload ist ein No-op mit Log-Fehler
- [x] Graceful Shutdown unverändert (SIGINT/SIGTERM)
- [x] `go test ./...` grün (73/73)

## Review-Fixes (Review 11–14)

- [x] **Data Race `Trigger.publishQOS`** (HIGH): Reads in `publishStatus`/`publishResult` laufen aus laufenden Command-Goroutinen, während der Reload sie unter Write-Lock tauscht ⇒ neuer `qos()`-Accessor unter RLock
- [x] **Data Race `SensorWorker.publishQOS`** (HIGH): analog — Write in `Initialise` unter `queryablesMu`, Reads über `qos()`
- [x] **CI mit `-race`**: `go test -race ./...` im Build-Step (GitHub-Runner haben gcc; lokal steht kein gcc zur Verfügung — der Race-Detector läuft erstmals in CI)
- [x] `signal.Stop` statt `defer close(channels)` (Send-on-closed-channel-Fenster beim Exit)
- [x] `applyWorkerConfig`-Reihenfolge: Queryables **nach** den Worker-Initialises (Mikro-Fenster dokumentiert)
- [x] Close-Timeout-Edge dokumentiert (waiter-Goroutine lebt bis zum nächsten Close — Commands sind process-group killed, seltener Randfall)
- [x] Stale HA-Discovery nach Reload (entfernte Entities behalten retained Discovery-Messages) im README dokumentiert

## Abhängigkeiten

06 (Webhook-Server-Lifecycle), 07 (Timeout/LookPath in der Config), 10 (Sensor-Optionen).
