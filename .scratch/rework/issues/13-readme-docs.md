# 08 - README & Docs

Status: resolved
Type: task

## Ziel

Dokumentation auf den Fork-Stand bringen: YAML-Config, Webhook-Nutzung, Betriebsmodi, Docker.

## Aufgaben

- [x] README komplett neu geschrieben: YAML-Config mit **allen** Features (availability, trigger inkl. timeout/stdin_payload, sensor inkl. publish_on_change, multi_sensor), Flags-Tabelle (alle 13 Flags), Webhook-API mit curl-Beispielen + Statuscode-Semantik (inkl. Timeout-entscheidung), MQTT-Semantik (STATE/RESULT), Hot-Reload (SIGHUP), Docker-Abschnitt mit den drei Einbindungsmuster + ehrlichen Limits (Container-Commands ≠ Host), Logging (text/json, kein Stacktrace bei Fatal), Development
- [x] Betriebsmodi dokumentiert (Broker-Modus vs. Webhook-only); `-device-id`-Hinweis für Container (machineid liefert dort meist "")
- [x] `startup/systemd` + `startup/openwrt`: `-config …/config.json` → `config.yaml`
- [x] Badge auf den Fork umgebogen (`desirevolution/mqtt-executor`); Release-Links auf Fork-Releases
- [x] Hinweis auf neues Log-Format (slog statt zap, kein Stacktrace bei Fatal) — Review-Fund 02/4 abgehakt

## Akzeptanzkriterien

- [x] README-Beispiele sind copy-paste-laufbar (YAML + curl gegen laufenden Server)
- [x] Keine Referenz mehr auf json5-Config oder pflichtige Broker-URL
- [x] Container-Limits + Einbindungsmuster dokumentiert (Grillrunde-Ergebnis)

## Akzeptanzkriterien

- README-Beispiele sind copy-paste-laufbar (YAML + curl gegen laufenden Server)
- Keine Referenz mehr auf json5-Config oder pflichtige Broker-URL

## Abhängigkeiten

06 (Endpoints/Flags final).
