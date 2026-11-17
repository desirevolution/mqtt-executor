# 05 - MQTT optional

Status: resolved
Type: task

## Ziel

Broker-URL optional machen: leere Broker-URL ⇒ reiner Webhook-Betrieb ohne MQTT-Connect; HA-Discovery schaltet sich automatisch ab.

## Aufgaben

- [x] `LoadConfig`: leere `-broker` zulassen; MQTT-Connect/-Loop nur bei gesetzter URL
- [x] Validation (fail fast): `sensor`/`multi_sensor` konfiguriert, aber keine Broker-URL ⇒ Startfehler (`validateBrokerRequirements` + Tests)
- [x] Trigger ohne Broker: zulassen; `Trigger.Initialise/Close` sind nil-safe (Subscribe/Unsubscribe entfallen), STATE/RESULT via `transport.NopPublisher` verworfen
- [x] `-home-assistant` ohne Broker: ignorieren + Warnung
- [x] Availability/LWT nur mit Broker anwenden (Warnung, wenn konfiguriert aber kein Broker)
- [x] Graceful Shutdown in beiden Modi sauber (Disconnect nur mit Client)
- [x] Smoke-Test: Start ohne Broker mit nur Triggern ⇒ Prozess idlet sauber, `Shutting down...` bei Signal. Anmerkung: `machineid.ID()` liefert in Containern oft "" (kein /etc/machine-id) — präexistent; `-device-id` explizit setzen, Doku in Ticket 13

## Akzeptanzkriterien

- Start ohne Broker, nur Trigger konfiguriert: Prozess läuft, Webhook (06) funktioniert später
- Start ohne Broker, Sensoren konfiguriert: klarer Validierungsfehler
- Start mit Broker: Verhalten unverändert gegenüber 03/04
- `go test ./...` grün inkl. Validierungstestfällen

## Abhängigkeiten

03 (Publisher-Interface, damit Publish-No-op möglich ist), 04 (YAML-Config).
