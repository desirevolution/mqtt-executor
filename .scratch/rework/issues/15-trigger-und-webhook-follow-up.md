# 15 - MQTT-Trigger, STOP/START und Webhook-Default

Status: resolved
Type: task

## Ziel

Die drei Befunde aus der Projektprüfung in der nächsten Entwicklungssession bearbeiten. Beide Funktionsfehler bei Triggern beheben und den Webhook standardmäßig lokal binden.

## Aufgaben

- [x] **MQTT-Trigger aktivieren.** Den verbundenen MQTT-Client in `cmd/mqtt-executor/main.go` an `trigger.MqttClient` übergeben, bevor die Trigger initialisiert werden. Start, Reconnect und Config-Reload bei aktiver Broker-Verbindung prüfen.
- [x] **STOP/START-Race beheben.** Aufräumen und abschließende Statusmeldungen in `internal/mqtt/trigger.go` an den konkreten Lauf binden. Wenn ein neuer Lauf bereits akzeptiert wurde, darf der alte ihn weder abbrechen noch dessen Status mit einem verspäteten `STOPPED` überschreiben.
- [x] **Webhook-Default auf Loopback setzen.** In `cmd/mqtt-executor/config.go` den Default für `-web` auf `127.0.0.1:8080` setzen. README und Beispiele prüfen. Das Docker-Beispiel verwendet weiterhin explizit `-web 0.0.0.0:8080`.

## Akzeptanzkriterien

1. Ein Broker-Test über den regulären Programmstart zeigt, dass konfigurierte Command-Topics abonniert werden, `START` den Befehl ausführt und `STOP` ihn abbricht. Nach Reconnect und Reload funktionieren die jeweils aktiven Trigger. HTTP-Trigger funktionieren auch im reinen Webhook-Modus.
2. Ein Regressionstest steuert die Reihenfolge von `STOP`, erneutem `START` und der verzögerten Rückkehr des alten Laufs. Der neue Lauf bleibt aktiv und kann separat gestoppt werden. `go test -race ./...` läuft durch.
3. Ohne `-web` ist die HTTP-API über `127.0.0.1:8080` erreichbar. Explizite Adressen, etwa `-web 192.168.178.42:8080` für Node-RED, werden übernommen. `-web=""` deaktiviert HTTP weiterhin. Das dokumentierte Docker-Beispiel bleibt über den veröffentlichten Port erreichbar.

## Bezug

05 (MQTT-optional), 06 (Webhook-Server und Trigger-Logik), 11 (Config-Hot-Reload).

## Verifikation

- MQTT-Trigger per Broker-Test beim Programmstart, nach Reconnect und nach SIGHUP geprüft. START und STOP funktionieren.
- STOP behält den Ausführungsplatz bis zum Ende des alten Laufs. Ein Regressionstest blockiert gezielt dessen Ergebnis-Publish und prüft die Ablehnung eines weiteren Starts.
- Loopback-Default im brokerlosen Programmstart geprüft. README und Dockerfile-Kommentar aktualisiert.
