# mqtt-executor

![Go](https://github.com/desirevolution/mqtt-executor/workflows/Go/badge.svg)

A small executor daemon with two ingresses and one purpose: **run configured
shell commands**.

- **MQTT ingress** (broker mode): subscribes to configured topics and executes
  a command whenever a message arrives (trigger). Interval-driven commands
  publish their output back to MQTT (sensor), including Home Assistant MQTT
  discovery.
- **Webhook ingress** (always on): a synchronous HTTP API — start/interrupt
  triggers and query sensors on demand, results in the HTTP response.
- **Broker optional**: with `-broker ""` the tool runs in webhook-only mode
  (no MQTT at all).

This is a hard fork of [rainu/mqtt-executor](https://github.com/rainu/mqtt-executor)
— reworked transport layer, YAML config, webhook API, metrics and a modern Go
baseline. Breaking changes vs. upstream are intentional.

# Get it

Build it yourself (requires [golang](https://golang.org/) ≥ 1.27):

```bash
go build -o mqtt-executor ./cmd/mqtt-executor/
```

Or grab the release binaries from
[releases](https://github.com/desirevolution/mqtt-executor/releases/latest) —
built for linux+windows × 386/amd64/arm/arm64. The `linux-arm` binary targets
**GOARM=6** explicitly, so it runs on armv6 *and* armv7 hardware (Raspberry Pi
Zero/1, older OpenWrt devices). Container images (multi-arch:
amd64/arm64/arm v7) live at `ghcr.io/desirevolution/mqtt-executor` (see
[Docker](#docker)).

# Configuration

Create a configuration file, default `./config.yaml`:

```yaml
# availability (broker mode only): LWT topic so Home Assistant knows
# whether this device is up
availability:
  topic: tele/__DEVICE_ID__/status
  payload:
    available: "On"
    unavailable: "Off"

trigger:
  - name: Touch file
    topic: cmnd/touch/file        # MQTT: send START / STOP here
    timeout: 30s                  # optional, interrupt after 30s
    stdin_payload: true           # optional: message body / webhook body → stdin
    command:
      name: /usr/bin/touch
      arguments: ["/tmp/example"]

sensor:
  - name: Free Memory
    topic: tele/__DEVICE_ID__/memory/free
    retained: false
    unit: kB                      # used for Home Assistant
    icon: hass:eye
    interval: 10s
    timeout: 5s                   # optional, interrupt after 5s
    publish_on_change: true       # optional: only publish when the output changes
    command:
      name: /bin/sh
      arguments: [-c, "cat /proc/meminfo | grep MemFree | cut -d: -f2 | sed 's/ //g' | grep -o [0-9]*"]

multi_sensor:
  - topic: tele/__DEVICE_ID__/stats
    retained: false
    interval: 10s
    command:
      name: /bin/sh
      arguments: [-c, "echo '{\"mem\":1, \"cpu\":2}'"]
    values:
      - name: Memory
        unit: kB
        icon: hass:eye
        template: "{{value_json.mem}}"   # used for Home Assistant
      - name: CPU
        unit: "%"
        icon: hass:eye
        template: "{{value_json.cpu}}"
```

Notes:

- `__DEVICE_ID__` in topics is replaced with the device id (see flags).
- The config is parsed **strictly**: unknown keys are a startup error (typos
  fail fast).
- All configured commands must exist and be executable at startup
  (`exec.LookPath` / path check) — missing commands abort the start.
- `interval` and `timeout` accept Go durations (`10s`, `1m30s`, …).

# Usage

```bash
# broker mode (MQTT + webhook)
mqtt-executor -broker tcp://127.0.0.1:1883 -config /path/to/config.yaml

# webhook-only mode (no MQTT)
mqtt-executor -config /path/to/config.yaml

# home assistant discovery (broker mode only)
mqtt-executor -broker tcp://127.0.0.1:1883 -config /path/to/config.yaml -home-assistant
```

| Flag | Default | Description |
|---|---|---|
| `-broker` | *(empty)* | Broker URI. **Optional** — empty means webhook-only mode. Sensors (interval-driven) require a broker; triggers work without one. |
| `-config` | `./config.yaml` | Topic configuration file (YAML). |
| `-web` | `:8080` | Webhook server listen address. Empty disables the server. |
| `-sub-qos` / `-pub-qos` | `1` / `1` | MQTT QoS levels (0, 1, 2). |
| `-user` / `-password` | *(empty)* | MQTT credentials. |
| `-client-id` | `mqtt-executor` | MQTT client id. |
| `-device-name` | auto | Device name for HA discovery. |
| `-device-id` | `machineid` (8 chars) | Unique device id, substitutes `__DEVICE_ID__`. **Set this explicitly in containers** — there is usually no `/etc/machine-id`. |
| `-home-assistant` | `false` | Enable HA MQTT discovery (broker mode only). |
| `-ha-discovery-prefix` | `homeassistant/` | Discovery topic prefix. |
| `-log-format` | `text` | `text` or `json` (stderr). |
| `-version` | — | Print the version and exit. |

# Webhook API

The webhook server is synchronous: HTTP responses carry the result, MQTT side
effects (STATE/RESULT, sensor publishes) still apply in broker mode. An
interactive playground is served at **`/docs`**, the machine-readable
description at **`/openapi.yaml`** — both embedded, no external assets.

```bash
# start a trigger and wait for the result
curl -X POST http://localhost:8080/trigger/Touch\ file
# → {"state":"STOPPED","result":"..."}

# pass a payload to the command (stdin_payload: true)
curl -X POST http://localhost:8080/trigger/github -d '{"action":"push"}'

# interrupt a running trigger (idempotent; alias: POST /trigger/<name>/stop)
curl -X DELETE http://localhost:8080/trigger/Touch\ file

# run a sensor command on demand (also published to MQTT, if broker)
curl http://localhost:8080/sensor/Free\ Memory

# health + prometheus metrics
curl http://localhost:8080/status
curl http://localhost:8080/metrics
```

Status codes: `200` finished/interrupted, `404` unknown name, `409` already
running, `500` command failed (result starts with `<FAILED>;`). A configured
timeout behaves like an intentional interrupt (`200`, `<INTERRUPTED>`); a
sensor timeout is reported as failure (`500`, `<FAILED>;…`) — sensors have no
interruption semantics.

# MQTT semantics (broker mode)

For a trigger on topic `cmnd/example`:

```bash
# start the command
mosquitto_pub -t cmnd/example -m "START"
# interrupt it
mosquitto_pub -t cmnd/example -m "STOP"
# read state and result
mosquitto_sub -t "cmnd/example/STATE" -t "cmnd/example/RESULT"
```

- `STATE`: `RUNNING` while the command runs, `STOPPED` otherwise
- `RESULT`: command output, `<INTERRUPTED>` (stop/timeout) or `<FAILED>;<error>`
- Only one run per trigger at a time; rapid `START`s don't race.
- With `-home-assistant`: switch + result/state entities are published via
  MQTT discovery; the webhook triggers the same entities. Note: entities of
  triggers/sensors removed via hot reload keep their retained discovery
  messages on the broker until HA restarts or they are manually removed.

# Hot reload

Send `SIGHUP` to re-read the configuration without a restart:

```bash
kill -HUP <pid>
```

Workers are stopped, the config is re-validated and applied, HA discovery is
refreshed. On any validation error the reload aborts and the old configuration
stays active. The webhook server, MQTT connection and availability stay
untouched by a reload.

# Docker

```bash
docker run -d --name mqtt-executor \
  -p 8080:8080 \
  -v ./config.yaml:/config.yaml:ro \
  ghcr.io/desirevolution/mqtt-executor:latest \
  -config /config.yaml -device-id my-device -web 0.0.0.0:8080
```

The image (alpine-based, ~18 MB) ships a `HEALTHCHECK` probing
`GET /status` (disable it with `--no-healthcheck` together with `-web ""` or
a non-default `-web` port). It runs as root by default — a deliberate choice
for an arbitrary-command executor; override with `docker run --user` if your
commands don't need it.

**Honest limits:** commands execute *inside the container* — `sensor` commands
reading `/proc` report container state, not host state; host automation
belongs on the host (binaries + systemd/openwrt units in `startup/`). Three
ways to bring your commands into the container:

1. **Extended image** — bake your scripts/tools into your own image:
   ```dockerfile
   FROM ghcr.io/desirevolution/mqtt-executor:latest
   COPY scripts/ /scripts/
   RUN apk add --no-cache curl jq
   ```
2. **Volumes** — mount scripts at runtime and reference them in the config.
3. **Binary embedding** — pull the static binary into an existing image that
   already has your tooling:
   ```dockerfile
   COPY --from=ghcr.io/desirevolution/mqtt-executor:latest /usr/local/bin/mqtt-executor /usr/local/bin/
   ```

# Logging

Structured `slog` output on stderr, `text` (default) or `json`
(`-log-format=json`). Note: unlike the old zap-based output there is no
stacktrace on fatal errors.

# Development

```bash
go test ./...     # unit tests
go vet ./...      # static analysis
go build ./...    # build
```

CI (`.github/workflows/`): tests + vet + cross-builds on push/PR; releases
(binaries + multi-arch image on ghcr) on `v*` tags.
