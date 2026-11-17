# 14 - OpenAPI-Spec + Playground-UI (A23)

Status: resolved
Type: task

## Ziel

Swagger-artige Web-Oberfläche für die Webhook-API: eine handgeschriebene OpenAPI-3-Spec unter `/openapi.yaml` plus eine kleine, eingebettete Playground-Seite unter `/docs` (go:embed) — offline-fähig, keine Dependencies, kein CDN.

## Aufgaben

- [x] `openapi.yaml` (handgeschrieben): POST /trigger/{name}, DELETE /trigger/{name} + POST /trigger/{name}/stop, GET /sensor/{name}, GET /status, GET /metrics — inkl. Response-Schemas (`{"state", "result"}`), Statuscodes (200/404/409/500), Payload→stdin-Hinweis
- [x] Test: Spec deckt alle registrierten Routes ab (`TestDocsEndpoints` — Gegenvergleich docs.html + geparste Spec), `/openapi.yaml` liefert 200 + parsebares YAML
- [x] `docs.html` (embedded): Endpoint-Liste mit Formularen (Trigger-Name, Payload-Textarea, Try-it-out-Buttons via fetch), Response-Viewer mit Statuscode + Body; keine externen Assets
- [x] Routes: `GET /docs` (HTML), `GET /openapi.yaml` (YAML) am Webhook-Server
- [x] E2E-Smoke im Container: /docs liefert HTML, /openapi.yaml liefert die Spec

## Review-Fixes (Review 11–14)

- [x] **400 + 1-MiB-Limit** in der Spec dokumentiert (MaxBytesReader im Handler, fehlte in der Beschreibung)

## Akzeptanzkriterien

- [x] Browser: /docs zeigt alle Endpunkte und erlaubt synchrone Ausführung gegen den laufenden Server
- [x] Offline nutzbar (kein CDN, keine externen Assets)
- [x] `go test ./...` grün

## Abhängigkeiten

06 (Webhook-Server, Endpoints final).
