# 01 - Fork-Setup

Status: open
Type: task

## Ziel

Echten GitHub-Fork von `rainu/mqtt-executor` unter dem Account des Users anlegen und das lokale Clone darauf umbiegen.

## Aufgaben

- [ ] `gh` CLI installieren (nicht vorhanden) und via Device-Flow authentifizieren (User-Interaktion nötig)
- [ ] `gh repo fork rainu/mqtt-executor --clone=false`
- [ ] Remote umbiegen: `origin` → Fork (push), `upstream` → rainu (fetch only)
- [ ] Arbeitsbranch für das Rework anlegen (z. B. `rework/v2`)

## Akzeptanzkriterien

- `gh repo view` zeigt den Fork aus dem Arbeitsverzeichnis
- `git remote -v` zeigt Fork als push-Ziel, rainu als upstream
- Bis dahin: alle Arbeit findet lokal im Clone statt (Tickets 02+ sind nicht blockiert)

## Abhängigkeiten

Keine — parallel zu 02 umsetzbar.
