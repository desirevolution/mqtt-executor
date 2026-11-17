package mqtt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePublisher struct {
	mu        sync.Mutex
	published []fakePublication
}

type fakePublication struct {
	topic   string
	payload string
}

func (f *fakePublisher) Publish(topic string, _ byte, _ bool, payload any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var payloadStr string
	switch v := payload.(type) {
	case []byte:
		payloadStr = string(v)
	case string:
		payloadStr = v
	default:
		payloadStr = fmt.Sprintf("%v", v)
	}

	f.published = append(f.published, fakePublication{
		topic:   topic,
		payload: payloadStr,
	})
	return nil
}

func (f *fakePublisher) count(topicSuffix, payload string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, p := range f.published {
		if strings.HasSuffix(p.topic, topicSuffix) && p.payload == payload {
			count++
		}
	}
	return count
}

// countTopic counts all publications to topics with the given suffix,
// regardless of the payload.
func (f *fakePublisher) countTopic(topicSuffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, p := range f.published {
		if strings.HasSuffix(p.topic, topicSuffix) {
			count++
		}
	}
	return count
}

// countResult counts result publications whose payload contains the given
// substring (raw command output usually has a trailing newline).
func (f *fakePublisher) countResult(topicSuffix, payloadSubstring string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, p := range f.published {
		if strings.HasSuffix(p.topic, topicSuffix) && strings.Contains(p.payload, payloadSubstring) {
			count++
		}
	}
	return count
}

// waitUntilStopped blocks until the given trigger has published STOPPED at
// least once or the timeout is reached.
func (f *fakePublisher) waitUntilStopped(t *testing.T, stateTopic string) {
	t.Helper()
	f.waitUntilStoppedCount(t, stateTopic, 1)
}

// waitUntilStoppedCount blocks until the given trigger has published STOPPED
// at least n times or the timeout is reached.
func (f *fakePublisher) waitUntilStoppedCount(t *testing.T, stateTopic string, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return f.count(stateTopic, PayloadStatusStopped) >= n
	}, 3*time.Second, 10*time.Millisecond)
}

type fakeMqttMessage struct {
	topic    string
	payload  []byte
	retained bool
}

func (f fakeMqttMessage) Ack()              {}
func (f fakeMqttMessage) Duplicate() bool   { return false }
func (f fakeMqttMessage) Qos() byte         { return 0 }
func (f fakeMqttMessage) Retained() bool    { return f.retained }
func (f fakeMqttMessage) Topic() string     { return f.topic }
func (f fakeMqttMessage) MessageID() uint16 { return 0 }
func (f fakeMqttMessage) Payload() []byte   { return f.payload }

func newTestTrigger(publisher transport.Publisher) *Trigger {
	return &Trigger{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: publisher,
	}
}

// TestTwoRapidStartsRunCommandOnce is the regression test for the race
// condition between isCommandRunning (RLock) and registerCommand (Lock): two
// rapid STARTs must run the command exactly once.
func TestTwoRapidStartsRunCommandOnce(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	triggerConf := config.Trigger{
		Name: "test",
		//the command runs long enough that the second START arrives while the
		//first run is still active
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 0.2; echo done"}},
	}
	triggerConf.Topic = "cmnd/test"
	tr.triggersByName = map[string]config.Trigger{triggerConf.Name: triggerConf}
	handler := tr.triggerHandler(triggerConf.Name, 0, false)

	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})
	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})

	pub.waitUntilStopped(t, "cmnd/test/STATE")

	assert.Equal(t, 1, pub.count("cmnd/test/STATE", PayloadStatusRunning), "exactly one RUNNING expected")
	assert.Equal(t, 1, pub.countResult("cmnd/test/RESULT", "done"), "exactly one RESULT expected")
	assert.Equal(t, 1, pub.count("cmnd/test/STATE", PayloadStatusStopped), "exactly one final STOPPED expected")
}

// TestStartAfterCompletionRunsAgain ensures the command registration is
// released after a run, so a new START executes the command again.
func TestStartAfterCompletionRunsAgain(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	triggerConf := config.Trigger{
		Name:    "test",
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "echo quick"}},
	}
	triggerConf.Topic = "cmnd/test"
	tr.triggersByName = map[string]config.Trigger{triggerConf.Name: triggerConf}
	handler := tr.triggerHandler(triggerConf.Name, 0, false)

	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})
	pub.waitUntilStopped(t, "cmnd/test/STATE")
	waitUntilTriggerIdle(t, tr, "test")

	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})
	pub.waitUntilStoppedCount(t, "cmnd/test/STATE", 2)

	assert.Equal(t, 2, pub.count("cmnd/test/STATE", PayloadStatusRunning), "two runs expected")
	assert.Equal(t, 2, pub.countResult("cmnd/test/RESULT", "quick"), "two results expected")
}

// TestInitialiseWithoutMqttClient ensures the trigger state machine is
// usable in webhook-only mode (no broker): Initialise/Close must not panic
// and still publish the initial STOPPED state through the Publisher.
func TestInitialiseWithoutMqttClient(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)

	tr.Initialise(1, 1, []config.Trigger{{
		Name:    "test",
		Topic:   "cmnd/test",
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "echo ok"}},
	}})
	assert.Equal(t, 1, pub.count("cmnd/test/STATE", PayloadStatusStopped), "initial STOPPED expected")

	assert.NoError(t, tr.Close(time.Second))
	assert.True(t, tr.IsInitialised())
}

func waitUntilTriggerIdle(t *testing.T, tr *Trigger, name string) {
	t.Helper()
	require.Eventually(t, func() bool {
		tr.lock.RLock()
		defer tr.lock.RUnlock()
		return tr.runningCommands[name] == nil
	}, time.Second, time.Millisecond)
}

// TestStopConcurrentWithCompletion stresses repeated stop requests while a
// run is completing, then verifies the trigger can reliably run again.
func TestStopConcurrentWithCompletion(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	triggerConf := config.Trigger{
		Name:    "test",
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 0.05; echo quick"}},
	}
	triggerConf.Topic = "cmnd/test"
	tr.triggersByName = map[string]config.Trigger{triggerConf.Name: triggerConf}
	handler := tr.triggerHandler(triggerConf.Name, 0, false)

	for i := 0; i < 50; i++ {
		handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})
		//hammer STOP while the command may finish at any moment
		for j := 0; j < 5; j++ {
			tr.Stop(triggerConf.Name)
		}
		pub.waitUntilStoppedCount(t, "cmnd/test/STATE", i+1)
		waitUntilTriggerIdle(t, tr, triggerConf.Name)
	}
}

// TestTriggerTimeoutInterruptsCommand ensures the optional per-trigger
// timeout interrupts a long-running command: the result is INTERRUPTED and
// the error is context.DeadlineExceeded (decision: a timeout behaves like an
// intentional cancellation).
func TestTriggerTimeoutInterruptsCommand(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	triggerConf := config.Trigger{
		Name:    "test",
		Topic:   "cmnd/test",
		Timeout: config.Interval(100 * time.Millisecond),
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 5; echo never"}},
	}
	tr.triggersByName = map[string]config.Trigger{"test": triggerConf}

	start := time.Now()
	state, result, err := tr.StartSync("test", nil)
	elapsed := time.Since(start)

	assert.True(t, errors.Is(err, context.DeadlineExceeded), "expected DeadlineExceeded, got %v", err)
	assert.Equal(t, PayloadStatusStopped, state)
	assert.Equal(t, PayloadResultInterrupted, result)
	assert.Less(t, elapsed, 3*time.Second, "timeout should interrupt, not wait for the command")

	//the result publishes follow the usual pattern
	assert.Equal(t, 1, pub.count("cmnd/test/STATE", PayloadStatusRunning), "one RUNNING expected")
	assert.Equal(t, 1, pub.count("cmnd/test/RESULT", PayloadResultInterrupted), "INTERRUPTED result expected")
	assert.Equal(t, 0, pub.countResult("cmnd/test/RESULT", "never"), "command output must not be published after timeout")
}

// TestInitialiseSwapsConfiguration ensures a re-Initialise (hot reload)
// swaps the trigger lookup atomically: the old trigger disappears, the new
// one is startable — even while lookups run concurrently.
func TestInitialiseSwapsConfiguration(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)

	first := []config.Trigger{{
		Name: "first", Topic: "cmnd/first",
		Command: config.Command{Name: "/bin/echo", Arguments: []string{"one"}},
	}}
	second := []config.Trigger{{
		Name: "second", Topic: "cmnd/second",
		Command: config.Command{Name: "/bin/echo", Arguments: []string{"two"}},
	}}

	tr.Initialise(1, 1, first)
	assert.True(t, tr.Exists("first"))
	assert.False(t, tr.Exists("second"))

	//concurrent lookups while swapping (no -race here, but this must at
	//least not panic and stay consistent)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_, _, _ = tr.StartSync("first", nil)
			_ = tr.Exists("second")
		}
		close(done)
	}()

	tr.Initialise(1, 1, second)
	assert.True(t, tr.Exists("second"))
	assert.False(t, tr.Exists("first"))

	_, _, err := tr.StartSync("first", nil)
	assert.ErrorIs(t, err, ErrUnknownTrigger)

	state, result, err := tr.StartSync("second", nil)
	assert.NoError(t, err)
	assert.Equal(t, PayloadStatusStopped, state)
	assert.Contains(t, result, "two")

	<-done
}

// TestStopInterruptsRunningCommand ensures a STOP payload cancels the running
// command and publishes the INTERRUPTED result.
func TestStopInterruptsRunningCommand(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	triggerConf := config.Trigger{
		Name:    "test",
		Command: config.Command{Name: "/bin/sh", Arguments: []string{"-c", "sleep 5; echo never"}},
	}
	triggerConf.Topic = "cmnd/test"
	tr.triggersByName = map[string]config.Trigger{triggerConf.Name: triggerConf}
	handler := tr.triggerHandler(triggerConf.Name, 0, false)

	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStart)})
	require.Eventually(t, func() bool {
		return pub.count("cmnd/test/STATE", PayloadStatusRunning) == 1
	}, 3*time.Second, 10*time.Millisecond, "command should be running")

	handler(nil, fakeMqttMessage{topic: "cmnd/test", payload: []byte(PayloadStop)})
	pub.waitUntilStopped(t, "cmnd/test/STATE")

	assert.Equal(t, 1, pub.count("cmnd/test/RESULT", "<INTERRUPTED>"), "INTERRUPTED result expected")
	assert.Equal(t, 0, pub.countResult("cmnd/test/RESULT", "never"), "command output must not be published after interruption")
}

type blockedTriggerResult struct {
	fakePublisher
	once       sync.Once
	publishing chan struct{}
	finish     chan struct{}
}

func (p *blockedTriggerResult) Publish(topic string, qos byte, retained bool, payload any) error {
	if result, ok := payload.(string); topic == "cmnd/test/RESULT" && ok && result == PayloadResultInterrupted {
		p.once.Do(func() {
			close(p.publishing)
			<-p.finish
		})
	}
	return p.fakePublisher.Publish(topic, qos, retained, payload)
}

func TestStopKeepsTriggerBusyUntilPreviousRunFinishes(t *testing.T) {
	pub := &blockedTriggerResult{publishing: make(chan struct{}), finish: make(chan struct{})}
	tr := newTestTrigger(pub)
	tr.Initialise(1, 1, []config.Trigger{{
		Name: "test", Topic: "cmnd/test", Command: config.Command{Name: "/bin/sleep", Arguments: []string{"30"}},
	}})
	t.Cleanup(func() { assert.NoError(t, tr.Executor.Close(time.Second)) })

	require.NoError(t, tr.StartAsync("test", nil))
	_, stopped := tr.Stop("test")
	require.True(t, stopped)
	select {
	case <-pub.publishing:
	case <-time.After(time.Second):
		close(pub.finish)
		t.Fatal("the interrupted run never began publishing its result")
	}

	assert.ErrorIs(t, tr.StartAsync("test", nil), ErrAlreadyRunning)
	close(pub.finish)
	require.Eventually(t, func() bool {
		err := tr.StartAsync("test", nil)
		return err == nil
	}, time.Second, time.Millisecond)
	_, stopped = tr.Stop("test")
	require.True(t, stopped)
	pub.waitUntilStoppedCount(t, "cmnd/test/STATE", 3)
}

func TestRunTopicUsesConfiguredFeedbackTopicAndIgnoresRetained(t *testing.T) {
	pub := &fakePublisher{}
	tr := newTestTrigger(pub)
	tr.Initialise(1, 1, []config.Trigger{{
		Name: "test", Topic: "cmnd/test", RunTopic: "cmnd/test/run", StdinPayload: true,
		Command: config.Command{Name: "/bin/cat"},
	}})

	handle := tr.subscriptions["cmnd/test/run"]
	handle(nil, fakeMqttMessage{topic: "cmnd/test/run", payload: []byte("replayed"), retained: true})
	handle(nil, fakeMqttMessage{topic: "cmnd/test/run", payload: []byte("STOP")})

	pub.waitUntilStoppedCount(t, "cmnd/test/STATE", 2)
	assert.Equal(t, 1, pub.count("cmnd/test/RESULT", "STOP"))
	assert.Equal(t, 0, pub.count("cmnd/test/RESULT", "replayed"))
	assert.Equal(t, 0, pub.countTopic("cmnd/test/run/RESULT"))
}
