# 04 - Config auf YAML

Status: resolved
Type: task

## Ziel

Config-Format von striktem JSON auf YAML umstellen (Breaking Change, im Fork erlaubt). 1:1-Spiegel der bisherigen Struktur.

## Aufgaben

- [x] `go.yaml.in/yaml/v3` als Abhängigkeit (gepflegter yaml.v3-Fork, schon als indirekte Dep vorhanden)
- [x] Config-Strukturen auf YAML-Tags umgestellt (`availability`, `trigger`, `sensor`, `multi_sensor` — Keys bleiben gleich)
- [x] `Interval`-Unmarshal: `UnmarshalYAML` mit `yaml.Node` (quoted + unquoted Durations)
- [x] **A1 Strict-Parsing**: `KnownFields(true)` — unbekannte Keys ⇒ Startfehler. Anmerkung: yaml.v3 KnownFields rekursiert nicht in ungetaggte Embedded-Structs ⇒ explizite `yaml:",inline"`-Tags an `Sensor`/`MultiSensor` (Workaround, empirisch verifiziert)
- [x] Default-Flag `-config` auf `./config.yaml` geändert
- [x] Validation-Logik unverändert übernommen (inkl. Topic-Charset-Regex, eindeutige Namen, `__DEVICE_ID__`-Substitution via `ReplaceAll`)
- [x] `topic_test.go` auf YAML portiert (20 Tabellenfälle inkl. neuer Fälle: unquoted interval, unbekannter Key, invalide YAML-Syntax)
- [ ] README-Config-Beispiele an YAML anpassen (finale Doku in Ticket 13)

## Akzeptanzkriterien

- `config.json` wird nicht mehr gelesen; `config.yaml` mit gleichem Inhalts-Spiegel funktioniert
- Portierter Validationstest grün (gleiche Fehlerfälle wie vorher)
- `go test ./...` grün

## Abhängigkeiten

02 (Baseline).
