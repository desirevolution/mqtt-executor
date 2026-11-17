package mqtt

import (
	"bytes"
	"context"
	"errors"
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

type triggerRun struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type Trigger struct {
	lock            sync.RWMutex
	statusMu        sync.Mutex // orders status snapshots with execution completion
	generation      uint64
	runningCommands map[string]*triggerRun
	triggersByName  map[string]config.Trigger
	subscriptions   map[string]MQTT.MessageHandler
	subscribeQOS    byte
	publishQOS      byte

	Executor   *cmd.CommandExecutor
	Publisher  transport.Publisher
	MqttClient MQTT.Client
}

// Initialise applies the configuration. Existing runs own their name until
// completion, including across a reload.
func (t *Trigger) Initialise(subscribeQOS, publishQOS byte, configs []config.Trigger) {
	t.lock.Lock()
	t.generation++
	t.subscribeQOS = subscribeQOS
	t.publishQOS = publishQOS
	t.subscriptions = make(map[string]MQTT.MessageHandler)
	t.triggersByName = make(map[string]config.Trigger, len(configs))
	for _, conf := range configs {
		t.triggersByName[conf.Name] = conf
		t.subscriptions[conf.Topic] = t.triggerHandler(conf.Name, t.generation, false)
		if conf.RunTopic != "" {
			t.subscriptions[conf.RunTopic] = t.triggerHandler(conf.Name, t.generation, true)
		}
	}
	t.lock.Unlock()

	t.ReInitialise()
}

// ReInitialise restores subscriptions and reports the current state on reconnect.
func (t *Trigger) ReInitialise() {
	t.statusMu.Lock()
	defer t.statusMu.Unlock()

	t.lock.RLock()
	subscriptions := t.subscriptions
	subQOS, pubQOS := t.subscribeQOS, t.publishQOS
	states := make(map[string]string, len(t.triggersByName))
	for name, conf := range t.triggersByName {
		state := PayloadStatusStopped
		if t.runningCommands[name] != nil {
			state = PayloadStatusRunning
		}
		states[conf.Topic] = state
	}
	t.lock.RUnlock()

	for topic, state := range states {
		t.publish(topic+"/"+TopicSuffixState, pubQOS, state)
	}
	if t.MqttClient != nil {
		for topic, handler := range subscriptions {
			t.MqttClient.Subscribe(topic, subQOS, handler)
		}
	}
}

func (t *Trigger) IsInitialised() bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.triggersByName != nil
}

func (t *Trigger) Exists(name string) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	_, ok := t.triggersByName[name]
	return ok
}

func (t *Trigger) StartSync(name string, payload []byte) (state, result string, err error) {
	conf, run, err := t.start(name, 0)
	if errors.Is(err, ErrAlreadyRunning) {
		return PayloadStatusRunning, "", err
	}
	if err != nil {
		return "", "", err
	}
	result, err = t.execute(conf, run, payload)
	return PayloadStatusStopped, result, err
}

func (t *Trigger) StartAsync(name string, payload []byte) error {
	return t.startAsync(name, payload, 0)
}

func (t *Trigger) startAsync(name string, payload []byte, generation uint64) error {
	conf, run, err := t.start(name, generation)
	if err != nil {
		return err
	}
	if conf.StdinPayload {
		payload = bytes.Clone(payload)
	}
	go t.execute(conf, run, payload)
	return nil
}

func (t *Trigger) start(name string, generation uint64) (config.Trigger, *triggerRun, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	conf, ok := t.triggersByName[name]
	if !ok || (generation != 0 && generation != t.generation) {
		return config.Trigger{}, nil, ErrUnknownTrigger
	}
	if t.runningCommands[name] != nil {
		return config.Trigger{}, nil, ErrAlreadyRunning
	}
	if t.runningCommands == nil {
		t.runningCommands = make(map[string]*triggerRun)
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &triggerRun{ctx: ctx, cancel: cancel}
	t.runningCommands[name] = run
	return conf, run, nil
}

func (t *Trigger) Stop(name string) (exists, stopped bool) {
	return t.stop(name, 0)
}

func (t *Trigger) stop(name string, generation uint64) (exists, stopped bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.triggersByName[name]; !ok || (generation != 0 && generation != t.generation) {
		return false, false
	}
	run := t.runningCommands[name]
	if run == nil || run.ctx.Err() != nil {
		return true, false
	}
	// The slot stays reserved until the process and its final publications
	// finish. STOP cannot make a replacement overlap with the old run.
	run.cancel()
	return true, true
}

func (t *Trigger) triggerHandler(name string, generation uint64, impulse bool) MQTT.MessageHandler {
	return func(_ MQTT.Client, message MQTT.Message) {
		if message.Retained() {
			return
		}
		if !impulse {
			switch strings.ToUpper(string(message.Payload())) {
			case PayloadStop:
				t.stop(name, generation)
				return
			case PayloadStart:
			default:
				slog.Warn("Invalid MQTT trigger payload", "topic", message.Topic())
				return
			}
		}
		if err := t.startAsync(name, message.Payload(), generation); errors.Is(err, ErrAlreadyRunning) {
			slog.Warn("Command is already running. Skip execution!", "trigger", name)
		}
	}
}

func (t *Trigger) execute(conf config.Trigger, run *triggerRun, payload []byte) (string, error) {
	defer t.finish(conf, run)

	ctx := run.ctx
	if conf.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(conf.Timeout))
		defer cancel()
	}
	var stdin io.Reader
	if conf.StdinPayload {
		stdin = bytes.NewReader(payload)
	}

	t.statusMu.Lock()
	t.publish(conf.Topic+"/"+TopicSuffixState, t.qos(), PayloadStatusRunning)
	t.statusMu.Unlock()

	output, err := metrics.Instrument(metrics.EntityTypeTrigger, conf.Name, func() ([]byte, error) {
		return t.Executor.ExecuteCommandWithStdin(conf.Command.Name, conf.Command.Arguments, ctx, stdin)
	})
	topic := conf.Topic + "/" + TopicSuffixResult
	switch {
	case err == nil:
		t.publish(topic, t.qos(), output)
		return string(output), nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		t.publish(topic, t.qos(), PayloadResultInterrupted)
		return PayloadResultInterrupted, err
	default:
		result := PayloadResultFailedPrefix + err.Error()
		t.publish(topic, t.qos(), result)
		return result, err
	}
}

func (t *Trigger) finish(conf config.Trigger, run *triggerRun) {
	defer run.cancel()
	t.statusMu.Lock()
	defer t.statusMu.Unlock()

	t.lock.RLock()
	current, configured := t.triggersByName[conf.Name]
	qos := t.publishQOS
	t.lock.RUnlock()

	t.publish(conf.Topic+"/"+TopicSuffixState, qos, PayloadStatusStopped)
	if configured && current.Topic != conf.Topic {
		t.publish(current.Topic+"/"+TopicSuffixState, qos, PayloadStatusStopped)
	}

	t.lock.Lock()
	if t.runningCommands[conf.Name] == run {
		delete(t.runningCommands, conf.Name)
	}
	t.lock.Unlock()
}

func (t *Trigger) publish(topic string, qos byte, payload any) {
	if err := t.Publisher.Publish(topic, qos, false, payload); err != nil {
		slog.Error("Failed to publish trigger update", "topic", topic, "error", err)
	}
}

func (t *Trigger) qos() byte {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.publishQOS
}

func (t *Trigger) Close(_ time.Duration) error {
	t.lock.Lock()
	subscriptions := t.subscriptions
	t.subscriptions = nil
	t.generation++
	t.lock.Unlock()

	if t.MqttClient != nil {
		for topic := range subscriptions {
			t.MqttClient.Unsubscribe(topic)
		}
	}
	return nil
}
