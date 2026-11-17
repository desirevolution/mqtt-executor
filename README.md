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
with these Linux targets:

| Binary | Target |
| --- | --- |
| `mqtt-executor-linux-amd64` | 64-bit x86 (Intel/AMD) |
| `mqtt-executor-linux-arm64` | 64-bit ARM (AArch64) |
| `mqtt-executor-linux-armv7` | 32-bit ARMv7 (`GOARM=7`) |
| `mqtt-executor-linux-armv6` | 32-bit ARMv6 (`GOARM=6`), e.g. Raspberry Pi Zero/1 |

Container images for `linux/amd64` and `linux/arm64` are available at
`ghcr.io/desirevolution/mqtt-executor` (see [Docker](#docker)).
Stable releases update the full version tag, the major.minor tag and `latest`.
Pre-releases publish their full version tag.

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
    webhook_id: touch-file        # HTTP: /trigger/touch-file
    run_topic: cmnd/touch/file/run # optional MQTT impulse with an optional payload
    timeout: 30s                  # optional, interrupt after 30s
    stdin_payload: true           # optional: message body / webhook body → stdin
    command:
      name: /usr/bin/touch
      arguments: ["/tmp/example"]

sensor:
  - name: Free Memory
    topic: tele/__DEVICE_ID__/memory/free
    query_topic: cmnd/__DEVICE_ID__/memory/query # optional MQTT request topic
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

- `topic` enables MQTT for a trigger. `webhook_id` enables its HTTP routes.
  Set both fields to expose both interfaces. Webhook IDs must be unique and
  use lowercase letters, digits or hyphens. Existing HTTP callers must switch
  from display names to explicitly configured webhook IDs.
- Omit both fields to disable a trigger while keeping its definition. Startup
  and reload log a warning. After a reload, an in-flight command may finish.
  Without `topic`, a configured `run_topic` is inactive.
- `__DEVICE_ID__` in topics is replaced with the device id (see flags).
- The config is parsed **strictly**: unknown keys are a startup error (typos
  fail fast).
- Commands for enabled triggers and sensors must exist and be executable at startup
  (`exec.LookPath` / path check) — missing commands abort the start.
- `interval` and `timeout` accept Go durations (`10s`, `1m30s`, …).
- Omit `interval` for an on-demand sensor. A single HTTP-only sensor needs
  only `name` and `command`. Periodic sensors and MQTT queries need a result
  `topic`; periodic sensors also need a configured broker.
- MQTT command and request topics must be unique and must not match any
  availability, state, or result topic.

# Usage

```bash
# broker mode (MQTT + webhook)
mqtt-executor -broker tcp://127.0.0.1:1883 -config /path/to/config.yaml

# webhook-only mode (no MQTT)
mqtt-executor -config /path/to/config.yaml

# validate YAML, topics and executable paths without starting the daemon
mqtt-executor -check-config -broker tcp://127.0.0.1:1883 -config /path/to/config.yaml

# home assistant discovery (broker mode only)
mqtt-executor -broker tcp://127.0.0.1:1883 -config /path/to/config.yaml -home-assistant
```

| Flag | Default | Description |
|---|---|---|
| `-broker` | *(empty)* | Broker URI. **Optional** — empty means webhook-only mode. Triggers and on-demand HTTP sensors work without a broker. Periodic sensors require a broker. |
| `-config` | `./config.yaml` | Topic configuration file (YAML). |
| `-check-config` | `false` | Validate configuration, including the selected broker mode and command paths, and exit. |
| `-web` | `127.0.0.1:8080` | Webhook listen address. Specify a LAN address for access from another machine. An empty value disables HTTP. |
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

The webhook API supports synchronous and asynchronous trigger starts.
HTTP trigger URLs use `webhook_id`. With both `topic` and a broker configured,
HTTP calls also publish trigger state and results to MQTT. An
interactive playground is served at **`/docs`**. Opening **`/`** redirects there.
Its dropdown lists the currently configured HTTP triggers by display name.
Reload the page after changing the trigger configuration. The generic
OpenAPI description is served at **`/openapi.yaml`**.

```bash
# start a trigger and wait for the result
curl -X POST 'http://127.0.0.1:8080/trigger/touch-file'
# → {"state":"STOPPED","result":"..."}

# start asynchronously (HTTP 202)
curl -i -X POST 'http://127.0.0.1:8080/trigger/touch-file/run'

# pass a payload to the command (stdin_payload: true)
curl -X POST http://127.0.0.1:8080/trigger/github -d '{"action":"push"}'

# interrupt a running trigger (idempotent; alias: POST /trigger/<id>/stop)
curl -X DELETE 'http://127.0.0.1:8080/trigger/touch-file'

# run a sensor command on demand (also published to MQTT, if broker)
curl 'http://127.0.0.1:8080/sensor/Free%20Memory'

# liveness, MQTT connection status, and Prometheus metrics
curl http://127.0.0.1:8080/status
curl http://127.0.0.1:8080/metrics
```

The synchronous start returns `200` when finished or interrupted.
`POST /trigger/{id}/run` returns `202` when accepted and has no execution
ID. Both start routes accept an optional raw body (up to 1 MiB) and report
`404` for unknown triggers or `409` while an execution is active. A failing
command returns `500` with `<FAILED>;` on synchronous calls. A trigger timeout
returns `<INTERRUPTED>`; a sensor timeout is a measurement failure. Use GET for
sensor queries. HEAD returns `405` without measuring.

`GET /status` remains a liveness probe. Its `mqtt` field is `disabled`,
`disconnected`, or `connected`. If a configured broker is unavailable at
startup, the daemon keeps serving HTTP and retries the MQTT connection in
the background.

# MQTT semantics (broker mode)

For a trigger on topic `cmnd/example`:

```bash
# start the command
mosquitto_pub -t cmnd/example -m "START"
# interrupt it
mosquitto_pub -t cmnd/example -m "STOP"
# read state and result
mosquitto_sub -t "cmnd/example/STATE" -t "cmnd/example/RESULT"

# once run_topic: cmnd/example/run is configured
mosquitto_pub -t cmnd/example/run -n
mosquitto_pub -t cmnd/example/run -m '{"value":42}'

# once query_topic: cmnd/memory/query is configured
mosquitto_pub -t cmnd/memory/query -n
```

- `STATE`: `RUNNING` while the command runs, `STOPPED` otherwise
- `RESULT`: command output, `<INTERRUPTED>` (stop/timeout) or `<FAILED>;<error>`
- Trigger results use the configured command topic as their base, including
  when a run was started via HTTP or `run_topic`.
- With `stdin_payload: true`, HTTP and `run_topic` pass the raw input to
  stdin. The existing command topic still passes the `START` message.
- A trigger holds its execution slot until the current run completes,
  including after a stop request. Further MQTT impulses are dropped.
- Trigger feedback is sent in order through a bounded background queue.
  A slow broker does not hold up command execution or HTTP responses. Updates
  can be dropped while offline or when the queue is full. Pending updates
  are cleared on reconnect, and the current trigger state is published again.
  Configured MQTT availability is published online after every connection.
- Sensor HTTP responses return once the measurement finishes. The MQTT
  result may still be awaiting delivery.
- Publish command, impulse, and query messages without retain. Replayed
  retained requests are ignored when subscribing or reconnecting.
- HTTP, MQTT and interval requests share an active sensor measurement.
  Both manual routes publish its result if a broker and result topic are
  configured, including when an unchanged result would otherwise be skipped.
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

The example binds HTTP to all interfaces inside the container so the
published port can reach it. Restrict the host-side port as needed, for
example with `-p 127.0.0.1:8080:8080` for host-local access.

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
