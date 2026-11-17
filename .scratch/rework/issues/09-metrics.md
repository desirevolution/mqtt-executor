# 09 - Prometheus-Metrics (B3)

Status: resolved
Type: task

## Ziel

Beobachtbarkeit über `GET /metrics` (Prometheus exposition format).

## Aufgaben

- [x] Abhängigkeit `github.com/prometheus/client_golang` v1.25.0 (neue direkte Dep — mit Feature-Entscheidung B3 abgesegnet)
- [x] Metriken (`internal/metrics`): `mqtt_executor_command_runs_total{type, name}`, `mqtt_executor_command_errors_total{type, name}`, `mqtt_executor_command_duration_seconds{type, name}` (Histogram 10ms–100s); `Instrument(entityType, name, run)`-Helper
- [x] Instrumentierung an den gemeinsamen Execute-Stellen (`trigger.executeCommand`, `sensor.executeSensor`); Entity-Namen: Trigger-Name, Single-Sensor-Name, Multi-Sensor ⇒ erster Value-Name (MultiSensoren haben keinen Config-Namen — `GeneralSensor.Name` wird beim Load befüllt, `yaml:"-"`)
- [x] Route `GET /metrics` am Webhook-Server (`promhttp.Handler()`)
- [x] Tests: `TestInstrument` (Run-/Error-Counter), `TestMetricsEndpoint` (Route + Metrik-Namen nach Run); E2E-Smoke: Histogrammzeilen mit `type="trigger"`/`name="echo"` auf dem echten Binary

## Akzeptanzkriterien

- [x] Nach Trigger-/Sensor-Ausführung erscheinen Run-/Durations-Metriken unter `/metrics`
- [x] Fehlerhafte Command-Ausführung zählt `errors_total`
- [x] `go test ./...` grün (65/65)

## Abhängigkeiten

06 (Webhook-Server als Host für die Route).
