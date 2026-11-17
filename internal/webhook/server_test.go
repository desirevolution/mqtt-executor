package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/mqtt"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	trigger := &mqtt.Trigger{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: transport.NopPublisher{},
	}
	trigger.Initialise(1, 1, []config.Trigger{
		{Name: "Display echo", WebhookID: "echo", Topic: "cmnd/echo", Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "echo done"}}},
		{Name: "Display slow", WebhookID: "slow", Topic: "cmnd/slow", Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 3; echo late"}}},
		{Name: "Display cat", WebhookID: "cat", Topic: "cmnd/cat", StdinPayload: true, Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "cat"}}},
		{Name: "Display failing", WebhookID: "failing", Topic: "cmnd/failing", Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "echo bad >&2; exit 1"}}},
		{Name: "Display timed", WebhookID: "timed", Topic: "cmnd/timed", Timeout: config.Interval(100 * time.Millisecond), Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 5; echo never"}}},
	})

	sensor := &mqtt.SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: transport.NopPublisher{},
	}
	sensor.Initialise(1, []config.GeneralSensor{{
		Name: "mem", Command: config.Command{Name: "/bin/echo", Arguments: []string{"42"}},
	}})
	t.Cleanup(func() { assert.NoError(t, sensor.Close(time.Second)) })
	t.Cleanup(func() { assert.NoError(t, trigger.Executor.Close(time.Second)) })

	srv := NewServer("", trigger, sensor, time.Now())
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

func TestTriggerStartSync(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/trigger/echo", "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body triggerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, mqtt.PayloadStatusStopped, body.State)
	assert.Contains(t, body.Result, "done")
}

func TestTriggerStartUnknown(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/trigger/unknown", "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestTriggerConcurrentStartConflict(t *testing.T) {
	ts := newTestServer(t)

	//start the slow trigger in the background (the request blocks until the
	//command finishes or is interrupted)
	firstResult := make(chan triggerResponse, 1)
	firstStatus := make(chan int, 1)
	go func() {
		resp, err := http.Post(ts.URL+"/trigger/slow", "", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		firstStatus <- resp.StatusCode
		var body triggerResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		firstResult <- body
	}()

	time.Sleep(300 * time.Millisecond) //let the command start

	//second start while running -> conflict
	second, err := http.Post(ts.URL+"/trigger/slow", "", nil)
	require.NoError(t, err)
	second.Body.Close()
	assert.Equal(t, http.StatusConflict, second.StatusCode)

	//stop the running command
	stop, err := http.NewRequest(http.MethodDelete, ts.URL+"/trigger/slow", nil)
	require.NoError(t, err)
	stopResp, err := http.DefaultClient.Do(stop)
	require.NoError(t, err)
	stopBody, _ := io.ReadAll(stopResp.Body)
	stopResp.Body.Close()
	assert.Equal(t, http.StatusOK, stopResp.StatusCode)
	assert.Contains(t, string(stopBody), mqtt.PayloadStatusStopped)

	//the first (blocked) request returns the interrupted result
	assert.Equal(t, http.StatusOK, <-firstStatus)
	first := <-firstResult
	assert.Equal(t, mqtt.PayloadResultInterrupted, first.Result)
}

func TestTriggerStopIdempotent(t *testing.T) {
	ts := newTestServer(t)

	//stopping a not-running but existing trigger is fine
	stop, err := http.NewRequest(http.MethodDelete, ts.URL+"/trigger/echo", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(stop)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTriggerStopUnknown(t *testing.T) {
	ts := newTestServer(t)

	stop, err := http.NewRequest(http.MethodDelete, ts.URL+"/trigger/unknown", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(stop)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestTriggerStartWithStdinPayload(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/trigger/cat", "", strings.NewReader("hello-payload"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body triggerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Contains(t, body.Result, "hello-payload")
}

// TestTriggerStartFailingCommand returns 500 when the command execution
// fails (exit code != 0), with the FAILED result in the body.
func TestTriggerStartFailingCommand(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/trigger/failing", "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var body triggerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, mqtt.PayloadStatusStopped, body.State)
	//on failure only the error is reported (not the command output), as before
	assert.Equal(t, mqtt.PayloadResultFailedPrefix+"exit status 1", body.Result)
}

func TestSensorQuery(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/sensor/mem")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	//the executor trims trailing whitespace/newlines from command output
	assert.Equal(t, "42", string(body))
}

func TestSensorQueryUnknown(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/sensor/unknown")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestStatusEndpoint(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Contains(t, body, "uptime")
}

// TestTriggerStartWithTimeout returns 200 with INTERRUPTED when the
// configured per-trigger timeout interrupts the command (not 500).
func TestTriggerStartWithTimeout(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/trigger/timed", "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body triggerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, mqtt.PayloadStatusStopped, body.State)
	assert.Equal(t, mqtt.PayloadResultInterrupted, body.Result)
}

// TestMetricsEndpoint ensures the metrics route exposes the executor metrics
// after at least one command run.
func TestMetricsEndpoint(t *testing.T) {
	ts := newTestServer(t)

	run, err := http.Post(ts.URL+"/trigger/echo", "", nil)
	require.NoError(t, err)
	run.Body.Close()
	require.Equal(t, http.StatusOK, run.StatusCode)

	resp, err := http.Get(ts.URL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "mqtt_executor_command_runs_total")
	assert.Contains(t, string(body), "mqtt_executor_command_duration_seconds")
}

// TestDocsEndpoints ensures the embedded API documentation is served and
// covers all registered endpoints.
func TestDocsEndpoints(t *testing.T) {
	ts := newTestServer(t)

	//the API surface (must match the mux routes in NewServer)
	endpoints := []string{
		"/trigger/{id}", "/trigger/{id}/run", "/trigger/{id}/stop", "/sensor/{name}", "/status", "/metrics",
	}

	docs, err := http.Get(ts.URL + "/docs")
	require.NoError(t, err)
	defer docs.Body.Close()
	require.Equal(t, http.StatusOK, docs.StatusCode)
	docsBody, err := io.ReadAll(docs.Body)
	require.NoError(t, err)

	spec, err := http.Get(ts.URL + "/openapi.yaml")
	require.NoError(t, err)
	defer spec.Body.Close()
	require.Equal(t, http.StatusOK, spec.StatusCode)

	specBody, err := io.ReadAll(spec.Body)
	require.NoError(t, err)

	var parsed struct {
		Paths map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(specBody, &parsed), "openapi.yaml must parse")

	for _, endpoint := range endpoints {
		assert.Contains(t, string(docsBody), endpoint, "docs must mention %s", endpoint)
		assert.Contains(t, parsed.Paths, endpoint, "openapi spec must document %s", endpoint)
	}
}

// TestServerCloseImplementsClosable ensures the server fits into the
// application shutdown closeable list.
func TestServerCloseImplementsClosable(t *testing.T) {
	srv := NewServer("", &mqtt.Trigger{}, &mqtt.SensorWorker{}, time.Now())
	var _ interface{ Close(time.Duration) error } = srv
	assert.NoError(t, srv.Close(time.Second))
}

func TestTriggerRunReturnsBeforeCompletionAndRejectsSecondStart(t *testing.T) {
	ts := newTestServer(t)
	url := ts.URL + "/trigger/slow"

	resp, err := http.Post(url+"/run", "", nil)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	resp, err = http.Post(url+"/run", "", nil)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	resp, err = http.Post(url+"/stop", "", nil)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTriggerRoutesRequireWebhookID(t *testing.T) {
	ts := newTestServer(t)
	for _, route := range []struct{ method, suffix string }{
		{http.MethodPost, ""},
		{http.MethodPost, "/run"},
		{http.MethodDelete, ""},
		{http.MethodPost, "/stop"},
	} {
		t.Run(route.method+route.suffix, func(t *testing.T) {
			req, err := http.NewRequest(route.method, ts.URL+"/trigger/Display%20echo"+route.suffix, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func TestDocsListsCurrentHTTPTriggers(t *testing.T) {
	trigger := &mqtt.Trigger{Publisher: transport.NopPublisher{}}
	handler := NewServer("", trigger, &mqtt.SensorWorker{}, time.Now()).Handler
	readDocs := func() string {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/docs", nil))
		require.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		return response.Body.String()
	}

	trigger.Initialise(1, 1, []config.Trigger{
		{Name: "Zebra", Topic: "cmnd/zebra", WebhookID: "zebra"},
		{Name: "A <b> & B", WebhookID: "first-hook"},
		{Name: "MQTT only", Topic: "cmnd/private"},
		{Name: "Disabled"},
	})
	beforeReload := readDocs()
	require.Contains(t, beforeReload, `<select id="trigger-id" required>`)
	first := `<option value="first-hook">A &lt;b&gt; &amp; B</option>`
	last := `<option value="zebra">Zebra</option>`
	require.Contains(t, beforeReload, first)
	require.Contains(t, beforeReload, last)
	assert.Less(t, strings.Index(beforeReload, first), strings.Index(beforeReload, last))
	assert.NotContains(t, beforeReload, "MQTT only")
	assert.NotContains(t, beforeReload, "Disabled")

	trigger.Initialise(1, 1, []config.Trigger{{Name: "Replacement", WebhookID: "replacement"}})
	afterReload := readDocs()
	assert.Contains(t, afterReload, `<option value="replacement">Replacement</option>`)
	assert.NotContains(t, afterReload, first)
	assert.NotContains(t, afterReload, last)

	trigger.Initialise(1, 1, []config.Trigger{{Name: "MQTT only", Topic: "cmnd/private"}})
	withoutWebhooks := readDocs()
	assert.Contains(t, withoutWebhooks, "No HTTP triggers configured.")
	assert.NotContains(t, withoutWebhooks, `<select id="trigger-id"`)
}

func TestRootRedirectsToDocsOnly(t *testing.T) {
	handler := NewServer("", &mqtt.Trigger{}, &mqtt.SensorWorker{}, time.Now()).Handler
	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusFound, root.Code)
	assert.Equal(t, "/docs", root.Header().Get("Location"))

	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/missing", nil))
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Empty(t, unknown.Header().Get("Location"))
}
