# 10 - Publish-on-change (B4)

Status: resolved
Type: task

## Ziel

Weniger MQTT-Chatter: (Multi-)Sensoren publishen optional nur, wenn sich der Command-Output ändert.

## Aufgaben

- [x] Config: `publish_on_change: true` pro (Multi-)Sensor (Default `false` = wie bisher)
- [x] SensorWorker refaktoriert: `executeSensor` publisht nicht mehr (nur noch ausführen + optionaler Timeout); Publish-Entscheidung bei den Callern — `runSensor` (Intervall, mit lastOutput/hasLast im Goroutine-Zustand, kein Lock) und `ExecuteOnce` (immer publishen)
- [x] Fehler-Outputs (`<FAILED>;...`) werden immer published (Änderungserkennung nur für Erfolgs-Outputs)
- [x] Manuelle Sensor-Abfrage per Webhook publishet unabhängig von der Option (Explizität zählt mehr als Chatter)
- [x] Tests: stabil ⇒ genau 1 Publish (~30 Ticks), wechselnder Output (`/dev/urandom`) ⇒ ≥ 10 Publishes, Default ⇒ publishet jeden Tick

## Review-Fixes (Review 07–10)

- [x] **Vergleichsbasis = letzter *publisheder* Payload** (statt letzter Erfolgs-Output): vorher blieb nach einem intermittierenden `<FAILED>` das alte A als Basis — ein Recovery zu A wurde fälschlich übersprungen und der Broker behielt den FAILED-Payload. Jetzt: FAILED-Payload wird als Basis übernommen ⇒ Recovery published wieder, wiederholte identische Fehler werden weiterhin unterdrückt
- [x] `lastPublished` wird nur mit `publish_on_change` getrackt (kein Output-Retention-Mehraufwand für Sensoren ohne die Option)
- [x] `errors_total`-Help-Text dokumentiert bewusst: zählt auch gecancelte/timed-out Runs (Entscheidung: nicht-erfolgreiche Ausführungen zählen, Doku im Help-String)

## Akzeptanzkriterien

- [x] Default-Verhalten unverändert (immer publishen)
- [x] Mit Option: nur bei Änderung + immer bei Fehlern
- [x] `go test ./...` grün (68/68)

## Abhängigkeiten

04 (Config-Feld), 05 (MQTT-optional — Publish-Ziel).
