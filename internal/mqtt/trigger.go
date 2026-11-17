package mqtt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/metrics"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

var (
	ErrUnknownTrigger = errors.New("unknown trigger")
	ErrAlreadyRunning = errors.New("command already running")
)

type Trigger struct {
	initialised     bool
	lock            sync.RWMutex
	runningCommands map[string]context.CancelFunc
	triggerConfigs  []config.Trigger
	triggersByName  map[string]config.Trigger
	subscriptions   map[string]subscription
	subscribeQOS    byte
	publishQOS      byte

	Executor  *cmd.CommandExecutor
	Publisher transport.Publisher
	// MqttClient is only needed for the MQTT ingress (Subscribe/Unsubscribe).
	// It is nil in webhook-only mode; all publishes run through the Publisher.
	MqttClient MQTT.Client
}

type subscription struct {
	trigger config.Trigger
	handler MQTT.MessageHandler
}

// Initialise (re-)applies the given trigger configuration. It is safe to
// call while the webhook server serves requests (hot reload): the lookups
// maps are swapped atomically under the lock, MQTT subscriptions and the
// initial state publishes happen outside the lock.
func (t *Trigger) Initialise(subscribeQOS, publishQOS byte, triggerConfigs []config.Trigger) {
	subscriptions := map[string]subscription{}
	triggersByName := map[string]config.Trigger{}
	for _, triggerConf := range triggerConfigs {
		triggersByName[triggerConf.Name] = triggerConf
		subscriptions[triggerConf.Name] = subscription{
			trigger: triggerConf,
			handler: t.createTriggerHandler(triggerConf),
		}
	}

	t.lock.Lock()
	t.subscribeQOS = subscribeQOS
	t.publishQOS = publishQOS
	t.triggerConfigs = triggerConfigs //safe the configs so that we can unsubscribe later (see Close func)
	t.subscriptions = subscriptions
	t.triggersByName = triggersByName
	t.runningCommands = map[string]context.CancelFunc{}
	t.initialised = true
	t.lock.Unlock()

	//MqttClient is nil in webhook-only mode: there is no MQTT ingress to
	//subscribe to, but the trigger state machine stays fully usable
	if t.MqttClient != nil {
		for _, sub := range subscriptions {
			t.MqttClient.Subscribe(sub.trigger.Topic, subscribeQOS, sub.handler)
		}
	}

	//publish the stopped state on startup
	for _, triggerConf := range triggerConfigs {
		t.publishStatus(triggerConf.Topic, PayloadStatusStopped)
	}
}

func (t *Trigger) IsInitialised() bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.initialised
}

// ReInitialise re-subscribes the MQTT ingress after a reconnect and republishes
// the current state. It is not used by the hot reload (that path calls
// Initialise with a new configuration).
func (t *Trigger) ReInitialise() {
	t.lock.RLock()
	subscriptions := make(map[string]subscription, len(t.subscriptions))
	for name, sub := range t.subscriptions {
		subscriptions[name] = sub
	}
	subscribeQOS := t.subscribeQOS
	t.lock.RUnlock()

	for triggerName, subscription := range subscriptions {
		if t.MqttClient != nil {
			t.MqttClient.Subscribe(subscription.trigger.Topic, subscribeQOS, subscription.handler)
		}

		//publish the current state on reinitialisation
		if t.isCommandRunning(triggerName) {
			t.publishStatus(subscription.trigger.Topic, PayloadStatusRunning)
		} else {
			t.publishStatus(subscription.trigger.Topic, PayloadStatusStopped)
		}
	}
}

// StartSync executes the named trigger's command and waits for its
// completion. The payload is passed to the command as stdin if the trigger is
// configured with stdin_payload. It returns the final state, the result
// payload (also published to MQTT when a broker is present) and the
// execution error, if any (context.Canceled for an intentional interruption).
func (t *Trigger) StartSync(name string, payload []byte) (state string, result string, err error) {
	triggerConf, ok := t.lookup(name)
	if !ok {
		return "", "", ErrUnknownTrigger
	}

	ctx, registered := t.tryRegisterCommand(triggerConf)
	if !registered {
		return PayloadStatusRunning, "", ErrAlreadyRunning
	}

	var stdin io.Reader
	if triggerConf.StdinPayload {
		stdin = bytes.NewReader(payload)
	}

	return t.executeCommand(triggerConf.Topic, triggerConf, ctx, stdin)
}

// Exists reports whether a trigger with the given name is configured.
func (t *Trigger) Exists(name string) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	_, ok := t.triggersByName[name]
	return ok
}

// Stop interrupts a running command of the named trigger. It reports whether
// the trigger exists and whether a running command was stopped.
func (t *Trigger) Stop(name string) (exists bool, stopped bool) {
	triggerConf, ok := t.lookup(name)
	if !ok {
		return false, false
	}
	return true, t.stopTrigger(triggerConf)
}

// lookup returns the trigger configuration with the given name.
func (t *Trigger) lookup(name string) (config.Trigger, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	triggerConf, ok := t.triggersByName[name]
	return triggerConf, ok
}

// stopTrigger interrupts and unregisters a running command of the given
// trigger configuration. Check, cancel and unregister are atomic under one
// write lock: otherwise the command could complete (and unregister itself)
// between a separate check and the interrupt, leaving a nil cancel func in
// the map.
func (t *Trigger) stopTrigger(trigger config.Trigger) bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	cancelFunc, exists := t.runningCommands[trigger.Name]
	if !exists {
		return false
	}

	cancelFunc()
	delete(t.runningCommands, trigger.Name)
	return true
}

func (t *Trigger) createTriggerHandler(triggerConfig config.Trigger) MQTT.MessageHandler {
	return func(client MQTT.Client, message MQTT.Message) {
		//payloads can contain sensitive data (see stdin_payload) - debug level only
		slog.Debug("Incoming message",
			"topic", message.Topic(),
			"payload", string(message.Payload()),
		)

		action := strings.ToUpper(string(message.Payload()))

		switch action {
		case PayloadStart:
			//ensure that only one trigger runs at the same time (check and
			//registration are atomic, see tryRegisterCommand)
			ctx, ok := t.tryRegisterCommand(triggerConfig)
			if !ok {
				slog.Warn("Command is already running. Skip execution!", "trigger", triggerConfig.Name)
				return
			}

			var stdin io.Reader
			if triggerConfig.StdinPayload {
				stdin = bytes.NewReader(message.Payload())
			}
			go func() {
				//async fire-and-forget: the result is published to MQTT, the
				//return values are only relevant for the synchronous (webhook) path
				_, _, _ = t.executeCommand(message.Topic(), triggerConfig, ctx, stdin)
			}()
		case PayloadStop:
			t.stopTrigger(triggerConfig)
		default:
			slog.Warn("Invalid payload. Do nothing.")
		}
	}
}

func (t *Trigger) isCommandRunning(triggerName string) bool {
	//we only need read access
	t.lock.RLock()
	defer t.lock.RUnlock()

	_, exist := t.runningCommands[triggerName]
	return exist
}

// tryRegisterCommand atomically checks whether a command for the given trigger
// is already running and, if not, registers it. This closes the gap between
// the former separate check (RLock) and registration (Lock) that allowed two
// rapid START messages to run the command concurrently.
func (t *Trigger) tryRegisterCommand(trigger config.Trigger) (context.Context, bool) {
	//we need write access
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, exists := t.runningCommands[trigger.Name]; exists {
		return nil, false
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	t.runningCommands[trigger.Name] = cancelFunc

	return ctx, true
}

func (t *Trigger) unregisterCommand(trigger config.Trigger) {
	//we need write access
	t.lock.Lock()
	defer t.lock.Unlock()

	//defensive: the command may already have been unregistered (e.g. by an
	//incoming STOP message) - do not call a nil cancel func
	if cancelFunc, exists := t.runningCommands[trigger.Name]; exists {
		//call the cancel func to release the resources
		cancelFunc()
		delete(t.runningCommands, trigger.Name)
	}
}

func (t *Trigger) executeCommand(topic string, trigger config.Trigger, ctx context.Context, stdin io.Reader) (state string, result string, execErr error) {
	defer t.unregisterCommand(trigger) //unregister at end (registration happened in tryRegisterCommand)

	//optional per-trigger timeout: the executor stays generic, the caller
	//applies the timeout to the execution context
	if trigger.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(trigger.Timeout))
		defer cancel()
	}

	t.publishStatus(topic, PayloadStatusRunning)       //publish that we are now running
	defer t.publishStatus(topic, PayloadStatusStopped) //at the end we are stopped

	output, execErr := metrics.Instrument(metrics.EntityTypeTrigger, trigger.Name, func() ([]byte, error) {
		return t.Executor.ExecuteCommandWithStdin(trigger.Command.Name, trigger.Command.Arguments, ctx, stdin)
	})
	switch {
	case execErr == nil:
		//publish the program's output (stdout & stderr)
		t.publishResult(topic, output)
		return PayloadStatusStopped, string(output), nil
	case errors.Is(execErr, context.Canceled), errors.Is(execErr, context.DeadlineExceeded):
		//this can happen if a STOPPED-Message was incoming, the timeout hit or
		//the application is shutting down
		t.publishResult(topic, PayloadResultInterrupted)
		return PayloadStatusStopped, PayloadResultInterrupted, execErr
	default:
		//program execution failed (status code != 0)
		t.publishResult(topic, PayloadResultFailedPrefix+execErr.Error())
		return PayloadStatusStopped, PayloadResultFailedPrefix + execErr.Error(), execErr
	}
}

func (t *Trigger) publishStatus(parentTopic, status string) {
	stateTopic := t.buildStateTopic(parentTopic)
	if err := t.Publisher.Publish(stateTopic, t.qos(), false, status); err != nil {
		slog.Error("Failed to publish trigger state", "topic", stateTopic, "error", err)
	}
}

func (t *Trigger) publishResult(parentTopic string, result any) {
	resultTopic := t.buildResultTopic(parentTopic)
	if err := t.Publisher.Publish(resultTopic, t.qos(), false, result); err != nil {
		slog.Error("Failed to publish trigger result", "topic", resultTopic, "error", err)
	}
}

// qos returns the current publish QoS (lock-guarded: running command
// goroutines read it while a hot reload may swap it).
func (t *Trigger) qos() byte {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.publishQOS
}

func (t *Trigger) buildStateTopic(parentTopic string) string {
	return fmt.Sprintf("%s/%s", parentTopic, TopicSuffixState)
}

func (t *Trigger) buildResultTopic(parentTopic string) string {
	return fmt.Sprintf("%s/%s", parentTopic, TopicSuffixResult)
}

func (t *Trigger) Close(timeout time.Duration) error {
	//copy the configs under the lock, then unsubscribe outside of it
	t.lock.RLock()
	triggerConfigs := make([]config.Trigger, len(t.triggerConfigs))
	copy(triggerConfigs, t.triggerConfigs)
	t.lock.RUnlock()

	//unsubscribe to all mqtt-topics (ignore the timeout!)
	if t.MqttClient != nil {
		for _, triggerConf := range triggerConfigs {
			t.MqttClient.Unsubscribe(triggerConf.Topic)
		}
	}

	return nil
}
