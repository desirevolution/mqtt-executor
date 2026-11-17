# 17 - MQTT und Webhooks pro Trigger aktivieren

Status: resolved
Type: task

## Ziel

Ein Trigger kann über MQTT, HTTP oder beide Schnittstellen erreichbar sein. Im HTTP-Pfad steht eine explizite `webhook_id`, während `name` der Anzeigename bleibt.

| `topic` | `webhook_id` | Verhalten |
| --- | --- | --- |
| gesetzt | fehlt | MQTT |
| fehlt | gesetzt | HTTP |
| gesetzt | gesetzt | MQTT und HTTP |
| fehlt | fehlt | Deaktiviert, Warnung beim Start und Reload |

## Aufgaben

- [x] `webhook_id` optional in der Trigger-Konfiguration unterstützen. IDs bestehen aus Kleinbuchstaben, Ziffern und Bindestrichen und sind unter Triggern eindeutig.
- [x] Sämtliche HTTP-Trigger-Routen (synchroner Start, asynchroner Start und beide Stop-Varianten) verwenden ausschließlich die `webhook_id`. Der Anzeigename ist keine alternative Route.
- [x] `topic` optional machen. Ohne `topic` gibt es für diesen Trigger keine MQTT-Abos, keine MQTT-Publikationen für `STATE` und `RESULT` und keinen Home-Assistant-Switch. Ein eventuell vorhandenes `run_topic` bleibt bis zur Wiederherstellung von `topic` inaktiv.
- [x] Ein Trigger ohne beide Felder bleibt in der Konfiguration. Beim Start oder Reload wird sein Name in einer Warnung ausgegeben. Die übrigen Trigger bleiben nutzbar.
- [x] Nach einem Reload mit deaktiviertem Trigger sind keine neuen Starts möglich. Eine bereits begonnene Ausführung läuft aus.
- [x] README, API-Dokumentation und OpenAPI anpassen. Bei der Migration benötigen bisher über den Anzeigenamen adressierte HTTP-Trigger eine `webhook_id` und die Aufrufer die neue URL.

## Akzeptanzkriterien

1. Alle vier Kombinationen der Tabelle sind mit aktivem MQTT und HTTP geprüft. Die globale Broker- und Webhook-Konfiguration gilt weiterhin.
2. `POST /trigger/timer-sound/run` adressiert einen Trigger mit dieser ID. Der Anzeigename und fehlende IDs sind über alle HTTP-Trigger-Routen unerreichbar. Ungültige und doppelte IDs liefern Konfigurationsfehler.
3. Entfernen von `topic` deaktiviert auch ein stehen gebliebenes `run_topic`. Fehlt zusätzlich `webhook_id`, wird beim Reload eine Warnung ausgegeben. Der noch laufende Befehl endet regulär und weitere Startversuche werden abgewiesen. Nach Wiederherstellung eines Feldes und Reload ist der Trigger über die entsprechende Schnittstelle wieder erreichbar.

## Bezug

15 (Trigger- und Reload-Korrekturen), 16 (asynchrone Trigger und MQTT-Impulstopic).

## Verifikation

- Broker-Integration mit MQTT-only, HTTP-only, beiden Schnittstellen und einem deaktivierten Trigger.
- Beim Reload werden MQTT- und HTTP-Einstiege zurückgezogen. Ein bereits gestarteter Prozess beendet seine Arbeit und der Trigger ist nach einem weiteren Reload wieder erreichbar.
- Race-Tests, Go-Vet und Cross-Builds für Linux amd64, arm64, armv7 und armv6 erfolgreich.
