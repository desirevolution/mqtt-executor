# 16 - Trigger und Sensoren über MQTT und HTTP

Status: resolved
Type: task

## Ziel

Trigger und Sensoren über beide Schnittstellen mit derselben Ausführungslogik anstoßen. MQTT publiziert Rückmeldungen auf den konfigurierten Topics. HTTP kann das Ergebnis direkt zurückgeben.

## Aufgaben

- [x] **Trigger auf Impuls ausführen.** Ein optionales `run_topic` nimmt leere MQTT-Nachrichten und rohe Nutzdaten an. Mit `stdin_payload: true` erhält der Befehl den unveränderten Payload. `POST /trigger/{name}/run` nimmt dieselben Eingaben an und antwortet bei Annahme sofort mit `202 Accepted`. Die erste Version verwendet keine Ausführungs-ID.
- [x] **Trigger-Verhalten teilen.** Pro Trigger läuft höchstens eine Ausführung, auch bei gemischten MQTT- und HTTP-Aufrufen. Währenddessen verwirft MQTT weitere Impulse und HTTP meldet `409 Conflict`. Timeout und Stop gelten für beide Wege. Das bestehende MQTT-Command-Topic mit `START` und `STOP`, der synchrone HTTP-Start, die HTTP-Stop-Routen und die Home-Assistant-Integration bleiben verfügbar.
- [x] **Sensoren auf Abruf anbieten.** Ein optionales MQTT-`query_topic` führt mit leerer Nachricht dieselbe Sensorabfrage aus wie `GET /sensor/{name}`. Die MQTT-Abfrage publiziert das Ergebnis auf `topic`. HTTP liefert die Ausgabe in der Antwort und publiziert sie zusätzlich, wenn ein Broker und ein Ergebnis-Topic konfiguriert sind.
- [x] **Intervalle optional machen.** Sensoren ohne `interval` laufen ausschließlich auf Abruf. Ein gesetztes Intervall aktiviert zusätzlich die periodische Messung. Ein einzelner Sensor im reinen HTTP-Betrieb lässt sich mit `name` und `command` konfigurieren. Für MQTT-Abfragen und periodische MQTT-Sensoren wird ein Ergebnis-Topic benötigt.
- [x] **Laufende Sensormessung gemeinsam nutzen.** HTTP- und MQTT-Abfragen für denselben Sensor verwenden eine bereits laufende Messung, auch wenn sie vom Intervall gestartet wurde. HTTP wartet auf das gemeinsame Ergebnis. Eine explizite MQTT-Abfrage publiziert das Ergebnis auch bei unverändertem Wert mit aktivem `publish_on_change`.
- [x] **MQTT-Rückmeldungen und Reconnect.** Trigger publizieren `STATE` und `RESULT` auf den bestehenden Topics, auch bei Start per HTTP oder `run_topic`. Beim Abonnieren oder Wiederverbinden erneut zugestellte gespeicherte Nachrichten lösen weder einen Impuls noch eine Sensorabfrage aus.
- [x] README, HTTP-Dokumentationsseite und OpenAPI aktualisieren.

## Akzeptanzkriterien

1. Ein leerer Impuls startet denselben Befehl über MQTT und HTTP. Ein Test zeigt außerdem, dass Nutzdaten über beide Impulswege unverändert an stdin ankommen, wenn die Option aktiv ist.
2. Ein asynchroner HTTP-Start antwortet mit `202`, während der Befehl noch läuft. Ein zweiter Start über MQTT oder HTTP führt zu keinem weiteren Lauf. HTTP meldet dabei `409`.
3. Manuelle Sensorabfragen verwenden dieselbe Messung. Ein reiner HTTP-Sensor funktioniert ohne Broker. Ein MQTT-Sensor publiziert nach einer Abfrage auf dem Ergebnis-Topic. Ein Sensor mit Intervall bleibt zusätzlich manuell abfragbar.
4. Eine laufende Messung, auch eine Intervallmessung, wird durch weitere HTTP- und MQTT-Abfragen nicht doppelt ausgeführt. HTTP liefert dasselbe Ergebnis zurück und MQTT publiziert es bei expliziter Abfrage auch bei unverändertem Wert mit `publish_on_change`.
5. Neue Impulswege verwenden die bestehenden Trigger-Rückmeldungen. Gespeicherte MQTT-Nachrichten starten nach dem Abonnieren oder Reconnect keine Ausführung.
6. Bestehende MQTT- und HTTP-Aufrufe sowie Home-Assistant-Discovery funktionieren weiterhin. `go test -race ./...` läuft durch.

## Abhängigkeit

15 (MQTT-Trigger aktivieren, STOP/START-Race beheben und Webhook-Default anpassen).

## Verifikation

- Broker-Test prüft MQTT- und HTTP-Impulse, rohe Nutzdaten, HTTP 202/409, bestehende Feedback-Topics und Home-Assistant-Discovery.
- HTTP-, MQTT- und Intervallabfragen teilen eine laufende Sensormessung. Geprüft wurden brokerlose Sensoren, Veröffentlichung unveränderter Werte bei expliziter Abfrage und das Ignorieren erneut zugestellter gespeicherter Nachrichten.
- README, API-Beispielseite und OpenAPI aktualisiert. Die Race-Tests, der Broker-Test und die vier Linux-Cross-Builds liefen durch.
