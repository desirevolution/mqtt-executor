# 12 - Docker / OCI-Image (A24, A25)

Status: resolved
Type: task

## Ziel

OCI-Image in die CI aufnehmen, damit der Webhook-Server typischerweise im Container läuft. Binaries bleiben weiterhin primärer Artefakt-Pfad.

## Aufgaben

- [x] Multi-Stage-Dockerfile: Build-Stage `golang:1.27-alpine` (`CGO_ENABLED=0`), Runtime-Stage **`alpine:3.21`** — revidiert von `scratch` nach Grillrunde + empirischem Befund (scratch hat kein Userland ⇒ **kein konfiguriertes Command ausführbar**, A3 `ValidateCommands` lehnte zurecht ab). Nutzer entschied: „Commands gehören ins Image" ⇒ drei Einbindungsmuster (Extended Image / Volume-Mounts / `COPY --from`), dokumentiert in Ticket 13
- [x] Version-Stamping: `-ldflags "-X main.versionTag=${VERSION}"` (Build-Arg); `versionString()` bevorzugt injected Tag vor BuildInfo — lokal verifiziert (`--version` ⇒ `v9.9.9-test`)
- [x] `EXPOSE 8080` + **HEALTHCHECK** via `wget --spider http://127.0.0.1:8080/status` (alpine hat wget; mit `-web=""` deaktiviert der Nutzer den Check mit)
- [x] Release-Workflow: `docker/build-push-action@v6` + QEMU, `linux/amd64, arm64, arm/v7`; Tags nur aus `v*` (A25): `latest` + Semver nach `ghcr.io/desirevolution/mqtt-executor`; Permission `packages: write`; Workflow-YAML validiert
- [x] `.dockerignore` (`.git`, `.scratch`, Binaries)

## Lokale Verifikation (buildx installiert)

- `docker buildx build --build-arg VERSION=…` ⇒ Image baut; Größe **18,2 MB**
- Container-Smoke (von innen, Port-Mapping in dieser Daemon-Umgebung nicht erreichbar): `/status` ⇒ ok, `POST /trigger/echo` ⇒ `hello-from-docker` **im Container ausgeführt**
- `--version` im Container zeigt das injected Tag
- Multi-Arch-Push + ghcr-Upload laufen erstmals beim ersten Tag-Push in den Fork

## Akzeptanzkriterien

- [x] Dockerfile baut lokal; Image läuft und führt Commands im Container aus
- [x] Release-Workflow veröffentlicht Image + Binaries; kein neues Release-Tooling
- [ ] Erster echter CI-Lauf nach Push (offen bis zum Push)

## Review-Fixes + Nachträge

- [x] **armv6-Binaries** (Nutzerentscheidung, 2026-11-17): CI-Build-Matrix arm ⇒ explizit `GOARM=6` (Go-Default für `GOARCH=arm` ist v7 — armv6-Geräte konnten das Binary nicht ausführen!). Ein v6-Binary läuft auf v6+v7 Hardware; verifiziert via `go version -m`. **Nur Binaries** — Docker-Plattformen bleiben amd64/arm64/arm v7 (Nutzerentscheidung „kein docker")
- [x] Dockerfile: `ca-certificates` (TLS-Broker-URIs mit System-Root-Store, Zukunftssicherheit trotz A6), `WORKDIR /etc/mqtt-executor`, Healthcheck-Kommentar korrigiert (`--no-healthcheck` / Compose `healthcheck.disable` — das behauptete `-health-cmd=none`-Flag existiert nicht)
- [x] Root-by-Default dokumentiert (bewusst für Arbitrary-Command-Executor, `--user`-Override erwähnt)

## Abhängigkeiten

06 (finale Flags/Endpoints), 08 (versionString für ldflags-Injection).
