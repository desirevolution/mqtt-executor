package webhook

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/rainu/mqtt-executor/internal/mqtt"
)

// static API documentation, embedded into the binary (offline-capable)
//
//go:embed openapi.yaml docs.html
var staticFiles embed.FS

const maxBodySize = 1 << 20 // 1 MiB

// Server exposes the synchronous HTTP ingress: trigger commands can be
// started and interrupted, sensor commands can be queried on demand.
// Results are returned in the HTTP response; MQTT side effects (STATE, RESULT,
// sensor publishes) stay shared with the MQTT ingress when a broker is
// configured.
type Server struct {
	http.Server

	Trigger   *mqtt.Trigger
	Sensor    *mqtt.SensorWorker
	startedAt time.Time
}

type triggerRequest struct {
	Action string `json:"action"`
}

type triggerResponse struct {
	State  string `json:"state"`
	Result string `json:"result,omitempty"`
}

// NewServer builds the webhook server for the given listen address
// ("" disables the server - it can still be closed harmlessly).
func NewServer(addr string, trigger *mqtt.Trigger, sensor *mqtt.SensorWorker, startedAt time.Time) *Server {
	s := &Server{
		Trigger:   trigger,
		Sensor:    sensor,
		startedAt: startedAt,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /trigger/{name}", s.handleTriggerStart)
	mux.HandleFunc("DELETE /trigger/{name}", s.handleTriggerStop)
	mux.HandleFunc("POST /trigger/{name}/stop", s.handleTriggerStop) //alias for DELETE
	mux.HandleFunc("GET /sensor/{name}", s.handleSensor)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /openapi.yaml", serveStatic("openapi.yaml", "application/x-yaml; charset=utf-8"))
	mux.HandleFunc("GET /docs", serveStatic("docs.html", "text/html; charset=utf-8"))

	s.Server = http.Server{
		Addr:    addr,
		Handler: mux,
		//note: no WriteTimeout on purpose - the synchronous endpoints may run
		//as long as the underlying command does (configure per-entity timeouts
		//in the config if needed)
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second, //bounds the body upload (<= 1 MiB)
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// handleTriggerStart starts the named trigger's command and waits for its
// completion. The request body is the payload (passed to the command as stdin
// if configured with stdin_payload).
func (s *Server) handleTriggerStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	//existence check before reading the body: reject unknown triggers
	//without consuming the (up to 1 MiB) payload
	if !s.Trigger.Exists(name) {
		httpError(w, http.StatusNotFound, fmt.Sprintf("unknown trigger %q", name))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("could not read request body: %v", err))
		return
	}

	state, result, err := s.Trigger.StartSync(name, body)
	switch {
	case errors.Is(err, mqtt.ErrAlreadyRunning):
		writeJSON(w, http.StatusConflict, triggerResponse{State: mqtt.PayloadStatusRunning})
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		//intentional interruption (DELETE/stop, configured timeout or
		//shutdown) is a success
		writeJSON(w, http.StatusOK, triggerResponse{State: state, Result: result})
	case err != nil:
		//the command execution failed (also published to MQTT)
		writeJSON(w, http.StatusInternalServerError, triggerResponse{State: state, Result: result})
	default:
		writeJSON(w, http.StatusOK, triggerResponse{State: state, Result: result})
	}
}

// handleTriggerStop interrupts a running command. It is idempotent: stopping
// a not-running (but existing) trigger responds with 200.
func (s *Server) handleTriggerStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	exists, stopped := s.Trigger.Stop(name)
	if !exists {
		httpError(w, http.StatusNotFound, fmt.Sprintf("unknown trigger %q", name))
		return
	}
	if stopped {
		slog.Info("Trigger stopped via webhook", "trigger", name)
	}
	writeJSON(w, http.StatusOK, triggerResponse{State: mqtt.PayloadStatusStopped})
}

// handleSensorQuery runs the named sensor's command synchronously. The output
// is also published to MQTT (if a broker is present).
func (s *Server) handleSensor(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	output, err := s.Sensor.ExecuteOnce(r.Context(), name)
	switch {
	case errors.Is(err, mqtt.ErrUnknownSensor):
		httpError(w, http.StatusNotFound, fmt.Sprintf("unknown sensor %q", name))
	case err != nil:
		//the failed result was already published (shared semantics)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s%s", mqtt.PayloadResultFailedPrefix, err.Error())
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(output)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(s.startedAt).Round(time.Second).String(),
	})
}

// Close gracefully shuts the server down within the given timeout. It
// implements the closable interface used by the application shutdown.
func (s *Server) Close(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Server.Shutdown(ctx)
}

// serveStatic serves an embedded API documentation file.
func serveStatic(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFiles.ReadFile(name)
		if err != nil {
			http.Error(w, "documentation file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("Failed to encode webhook response", "error", err)
	}
}

func httpError(w http.ResponseWriter, status int, message string) {
	//plain text error responses keep curl output readable
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	http.Error(w, message, status)
}
