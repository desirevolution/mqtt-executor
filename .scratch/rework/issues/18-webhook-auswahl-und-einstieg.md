# 18 - Webhook-Auswahl in der API-Dokumentation

Status: resolved
Type: task

## Ziel

In `/docs` lässt sich ein konfigurierter HTTP-Trigger auswählen. Ein Aufruf von `/` führt zur Dokumentation.

## Akzeptanzkriterien

1. Das Dropdown zeigt die Anzeigenamen der Trigger mit einer aktiven `webhook_id`. MQTT-only und deaktivierte Trigger fehlen. Bei einer leeren Liste gibt es einen sichtbaren Leerzustand.
2. Start, Start im Hintergrund und Stop verwenden die ID des gewählten Triggers. Nach einem Seitenaufruf nach Konfigurations-Reload ist die Liste aktuell.
3. `GET /` leitet auf `/docs` weiter. Unbekannte Pfade behalten ihre 404-Antwort.
4. `/openapi.yaml` beschreibt weiterhin generische Routen mit `{id}`.

## Abhängigkeit

17 (explizite `webhook_id`).

## Verifikation

- Race-Tests und Go-Vet erfolgreich. Die vier Linux-Cross-Builds laufen durch.
- Test am laufenden Executor prüft die Weiterleitung und die aktualisierte Auswahl nach SIGHUP. Tests prüfen den Leerzustand und 404 für unbekannte Pfade.
- Die drei Aktionen verwenden die ausgewählte Webhook-ID. OpenAPI bleibt generisch.
