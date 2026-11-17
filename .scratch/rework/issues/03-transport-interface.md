# 03 - Transport-Interface

Status: resolved
Type: task

## Ziel

Den hart verdrahteten paho-`MQTT.Client` hinter ein Interface abstrahieren, damit MQTT zu einer austauschbaren Implementierung wird und der Webhook (Ticket 06) denselben Publisher nutzen kann.

## Aufgaben

- [x] Interface in `internal/transport`:
  ```go
  type Publisher interface {
      Publish(topic string, qos byte, retained bool, payload any) error
  }
  ```
  (Subscribe wird nur vom MQTT-Trigger-Ingress gebraucht: `Trigger` behält den paho-Client nur für Subscribe/Unsubscribe)
- [x] MQTT-Implementierung als Wrapper inkl. `token.Wait()`-Handling — Publish-Tokens sind jetzt überall geprüft; `PublishDiscoveryConfig` propagiert Publish-Fehler mit Topic-Kontext
- [x] Umshichteln: `main.go`, `internal/mqtt/trigger.go`, `sensor.go`, `status.go`, `hassio/client.go` auf Interface umgestellt
- [x] **Race-Fix**: `tryRegisterCommand` (atomarer Check+Register) ersetzt getrenntes `isCommandRunning`/`registerCommand` — Regressionstest `TestTwoRapidStartsRunCommandOnce`
- [x] **Zusatz (durch Tests aufgedeckt, präexistente Bugs):**
  - `unregisterCommand` defensiv: STOP-Handler + deferred Unregisterriefen zweimal — zweiter Zugriff auf gelöschten Map-Eintrag = nil-Cancel-Func = **Panic** im Goroutine (Original-Bug)
  - Process-Group-Kill (unix: `Setpgid` + `Cancel` = `kill(-pid, SIGKILL)`, windows: Default) + `WaitDelay` 2s: ohne das kehrte `CombinedOutput` nach Cancel **nie** zurück, wenn ein Shell-Kommando Enkelprozesse spawn (diese halten die Output-Pipes offen → kein EOF). Betroffen waren STOP und Shutdown. Tests: `TestStopInterruptsRunningCommand`
- [x] HA-Discovery-Payload-Struktur unverändert lassen (Nicht-Ziel aus Spec)

## Akzeptanzkriterien

- Kein paho-Typ außerhalb der MQTT-Implementierung sichtbar
- `go test ./...` grün inkl. neuem Race-Regressionstest
- Verhalten gegenüber 02 unverändert (gleiche Topics, gleiche Payloads)

## Abhängigkeiten

02 (Modem-Baseline, damit Deps/APIs aktuell sind).
