# 07 - Command-Hardening (A2 Timeout, A3 LookPath)

Status: resolved
Type: task

## Ziel

Commands absichern: optionales Timeout pro Trigger/Sensor und Command-Existenzprüfung beim Start.

## Aufgaben

- [x] Config: optionales Feld `timeout` pro Trigger und (Multi-)Sensor (Duration-Syntax wie `interval`, Default: kein Timeout); negative Timeouts ⇒ Validierungsfehler
- [x] Executor bleibt generisch: Aufrufer (Trigger `executeCommand`, Sensor `executeSensor`) wrappen den Execution-Context mit `context.WithTimeout`
- [x] Timeout-Ablauf verhält sich wie ein Cancel: Result `<INTERRUPTED>` + State `STOPPED` (`errors.Is` auf `DeadlineExceeded` **und** `Canceled`), 500-HTTP-Mapping greift nicht (INTERRUPTED ist 200)
- [x] Command-Existenzprüfung beim Start: `TopicConfigurations.ValidateCommands()` (LookPath für bare names; absolute/relative Pfade: existiert + ausführbar, Verzeichnisse abgelehnt); von `LoadConfig` nach dem Config-Load aufgerufen
- [x] Tests: Timeout-Config-Fälle (ok/negativ), `ValidateCommands` (7 Fälle + alle Entity-Typen), Runtime-Timeout `TestTriggerTimeoutInterruptsCommand`

## Akzeptanzkriterien

- [x] Trigger/Sensor ohne `timeout` verhalten sich wie bisher
- [x] Mit `timeout`: Command wird bei Ablauf abgebrochen, STATE/RESULT folgen dem bestehenden Muster
- [x] Start mit nicht existierendem Command bricht mit klarer Fehlermeldung ab
- [x] `go test ./...` grün (60/60)

## Review-Fixes (Review 07–10)

- [x] **Trigger-Timeout am Webhook ⇒ 200 statt 500**: `DeadlineExceeded` in den Interrupted-200-Fall aufgenommen; Test `TestTriggerStartWithTimeout`
- [x] **Wiring-Bug**: `validateBrokerRequirements` lief **vor** dem Config-Load gegen den Zero-Value (Sensoren-ohne-Broker wurde nie rejected — die Unit-Tests griffen nur direkt auf die Funktion zu). Jetzt: Config-Load zuerst, danach Modus-Validierung; Smoke verifiziert (Sensor ohne Broker ⇒ exit 1, Trigger-only ⇒ läuft)
- [x] **Entscheidung Sensor-Timeout** (bewusst divergent zum Trigger): Sensor-Timeout ⇒ `<FAILED>;context deadline exceeded` + HTTP 500 — Sensoren haben keine Start/Stop-Semantik, ein abgebrochener Sensor-Run hat keinen Wert geliefert; Trigger-Timeout bleibt `<INTERRUPTED>`/200. Dokumentiert in Ticket 13 (README)
- [x] ValidateCommands: deterministische Fehlerreihenfolge (sortierte Namen)

## Abhängigkeiten

04 (YAML-Config-Felder).
