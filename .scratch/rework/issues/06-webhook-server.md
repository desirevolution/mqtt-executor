# 06 - Webhook-Server

Status: resolved
Type: task

## Ziel

Synchronen HTTP-Ingress neben MQTT einführen: Trigger starten/stoppen und Sensoren abfragen. One way — Antworten nur in der HTTP-Response, kein Push-Output.

## Aufgaben

- [x] `net/http`-Server (stdlib, Go-1.22+-Routing) in neuem Package `internal/webhook`, Flag `-web` (Default `:8080`, `""` deaktiviert)
- [x] `POST /trigger/{name}`: **Body = Payload**, Command starten und synchron auf Ende warten; Stop contract geändert gegen früheren Spec-Entwurf: `DELETE /trigger/{name}` (+ Alias `POST /trigger/{name}/stop`) statt `{"action":"stop"}`-Body — der POST-Body gehört dem Payload→stdin-Feature (B1), Body und Action-JSON würden kollidieren. Spec aktualisiert.
- [x] `GET /sensor/{name}`: Command ausführen, Output zurück; Publish nach MQTT, wenn Broker (geteilte Semantik). Queryables-Map: Single-Sensoren unter Name, Multi-Sensor-Werte unter Value-Name (geteilter Output)
- [x] `stdin_payload: true` (B1): Body bzw. MQTT-Payload → stdin (`Executor.ExecuteCommandWithStdin`); gilt für MQTT- UND Webhook-Trigger
- [x] `GET /status`: Status + Uptime (B2)
- [x] Trigger-Logik geteilt: `Trigger.StartSync(name, payload)` (synchron, für HTTP) und MQTT-Handler (asynchron) nutzen dieselbe `executeCommand`; mit Broker publishet der Webhook-Start STATE/RESULT nach MQTT
- [x] Response: JSON `{"state", "result"}` (Trigger) / Plain-Text-Output (Sensor); 404 unbekannt, 409 bereits RUNNING, 500 Command-Fehler, 400 Body-Overflow
- [x] Bind-Warnung bei Bind auf alle Interfaces (`""`, `0.0.0.0`, `::`)
- [x] Graceful Shutdown: Server in der `closeables`-Liste. Anmerkung (Review-Korrektur): closeables laufen **parallel** mit gemeinsamem Timeout — die List-Position ist kosmetisch, nicht sequenziell. In-flight Sync-Requests enden trotzdem zügig, weil `commandExecutor.Close` parallel alle laufenden Commands cancelt
- [x] Tests: 11 Server-Tests (Sync-Start, 409-Conflict, 500 bei Command-Fehler, Stop idempotent/unknown, stdin_payload, Sensor-Query, 404, /status, Close-Interface) + E2E-Smoke gegen den Binary

## Review-Fixes (Review 04–06)

- [x] **500 bei Command-Fehlern** (statt 200): `executeCommand` gibt jetzt `(state, result, execErr)` zurück; HTTP mappt Nicht-Cancel-Fehler auf 500 mit JSON-Body, `context.Canceled` (INTERRUPTED) bleibt 200. Test: `TestTriggerStartFailingCommand`
- [x] **Nil-Panic-Race in stopTrigger**: check+cancel+delete atomar unter Write-Lock; totes `interruptCommand` entfernt. Stresstest: `TestStopConcurrentWithCompletion` (250 Stops gegen laufende Commands)
- [x] **Multi-Sensor-Queryables**: Review-Verdacht geprüft und **verworfen** — MultiSensoren haben im Original einen gemeinsamen ResultTopic (Values haben keine eigenen Topics); Publish auf den Parent-Topic ist korrekt
- [x] **ExecuteOnce mit `r.Context()`**: abgebrochene HTTP-Requests canceln das Sensor-Command; Shutdown war bereits abgedeckt (`commandExecutor.Close` cancelt alle registrierten Contexts)
- [x] **Server-Timeouts**: `ReadTimeout` 30s + `IdleTimeout` 120s; **kein** WriteTimeout (Sync-Endpoints laufen solange das Command läuft — bewusst, dokumentiert im Code)
- [x] **Payload-Log auf Debug-Level** (stdin_payload kann sensible Daten enthalten)
- [x] **Body-Lesung nach 404-Check**: `Trigger.Exists(name)` vor dem `io.ReadAll`
- [x] **Bind-Fehler fatal**: ListenAndServe-Fehler (z. B. Port belegt) beendet den Prozess statt still weiterzulaufen

## Akzeptanzkriterien

- Beide Endpoints funktionieren mit und ohne Broker gemäß Modus-Matrix in der Spec
- `go test ./...` grün inkl. Webhook-Tests
- Server lässt sich per Flag deaktivieren; Default-Bind dokumentiert

## Abhängigkeiten

03 (Publisher), 04 (YAML-Config), 05 (MQTT-optional).
