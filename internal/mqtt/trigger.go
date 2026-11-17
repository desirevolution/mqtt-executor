package mqtt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
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
	webhookNames    map[string]string // webhook ID to configured trigger name
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
	t.statusMu.Lock()
	t.lock.Lock()
	t.generation++
	t.subscribeQOS = subscribeQOS
	t.publishQOS = publishQOS
	t.subscriptions = make(map[string]MQTT.MessageHandler)
	t.triggersByName = make(map[string]config.Trigger, len(configs))
	t.webhookNames = make(map[string]string)
	for _, conf := range configs {
		t.triggersByName[conf.Name] = conf
		if conf.WebhookID != "" {
			t.webhookNames[conf.WebhookID] = conf.Name
		}
		if conf.Topic == "" {
			if conf.WebhookID == "" {
				slog.Warn("Trigger disabled: neither topic nor webhook_id is configured", "trigger", conf.Name)
			}
			continue
		}
		t.subscriptions[conf.Topic] = t.triggerHandler(conf.Name, t.generation, false)
		if conf.RunTopic != "" {
			t.subscriptions[conf.RunTopic] = t.triggerHandler(conf.Name, t.generation, true)
		}
	}
	t.lock.Unlock()
	t.statusMu.Unlock()

	t.ReInitialise()
}

// ReInitialise restores subscriptions and reports the current state on reconnect.
func (t *Trigger) ReInitialise() {
	if t.MqttClient != nil && !t.MqttClient.IsConnectionOpen() {
		return
	}
	t.statusMu.Lock()

	t.lock.RLock()
	subscriptions := t.subscriptions
	subQOS, pubQOS := t.subscribeQOS, t.publishQOS
	states := make(map[string]string, len(t.triggersByName))
	for name, conf := range t.triggersByName {
		if conf.Topic == "" {
			continue
		}
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
	t.statusMu.Unlock()
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

// WebhookTrigger is the display name and URL ID of an HTTP-enabled trigger.
type WebhookTrigger struct {
	ID   string
	Name string
}

// Webhooks returns a sorted snapshot of the active HTTP trigger configuration.
func (t *Trigger) Webhooks() []WebhookTrigger {
	t.lock.RLock()
	hooks := make([]WebhookTrigger, 0, len(t.webhookNames))
	for id, name := range t.webhookNames {
		hooks = append(hooks, WebhookTrigger{ID: id, Name: name})
	}
	t.lock.RUnlock()

	slices.SortFunc(hooks, func(a, b WebhookTrigger) int {
		return strings.Compare(a.Name, b.Name)
	})
	return hooks
}

func (t *Trigger) HasWebhook(id string) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	_, ok := t.webhookNames[id]
	return ok
}

func (t *Trigger) StartSync(webhookID string, payload []byte) (state, result string, err error) {
	conf, run, err := t.start(webhookID, 0)
	if errors.Is(err, ErrAlreadyRunning) {
		return PayloadStatusRunning, "", err
	}
	if err != nil {
		return "", "", err
	}
	result, err = t.execute(conf, run, payload)
	return PayloadStatusStopped, result, err
}

func (t *Trigger) StartAsync(webhookID string, payload []byte) error {
	return t.startAsync(webhookID, payload, 0)
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

func (t *Trigger) start(key string, generation uint64) (config.Trigger, *triggerRun, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	conf, ok := t.resolveTrigger(key, generation)
	if !ok {
		return config.Trigger{}, nil, ErrUnknownTrigger
	}
	if t.runningCommands[conf.Name] != nil {
		return config.Trigger{}, nil, ErrAlreadyRunning
	}
	if t.runningCommands == nil {
		t.runningCommands = make(map[string]*triggerRun)
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &triggerRun{ctx: ctx, cancel: cancel}
	t.runningCommands[conf.Name] = run
	return conf, run, nil
}

func (t *Trigger) Stop(webhookID string) (exists, stopped bool) {
	return t.stop(webhookID, 0)
}

func (t *Trigger) stop(key string, generation uint64) (exists, stopped bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	conf, ok := t.resolveTrigger(key, generation)
	if !ok {
		return false, false
	}
	run := t.runningCommands[conf.Name]
	if run == nil || run.ctx.Err() != nil {
		return true, false
	}
	// The slot stays reserved until the process and its final publications
	// finish. STOP cannot make a replacement overlap with the old run.
	run.cancel()
	return true, true
}

// resolveTrigger is called with lock held. HTTP resolves by its explicit ID;
// MQTT handlers carry the configuration generation they were registered for.
func (t *Trigger) resolveTrigger(key string, generation uint64) (config.Trigger, bool) {
	if generation == 0 {
		name, ok := t.webhookNames[key]
		if !ok {
			return config.Trigger{}, false
		}
		conf, ok := t.triggersByName[name]
		return conf, ok
	}
	conf, ok := t.triggersByName[key]
	return conf, ok && generation == t.generation && conf.Topic != ""
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
	t.publishTrigger(conf.Name, TopicSuffixState, PayloadStatusRunning)
	t.statusMu.Unlock()

	output, err := metrics.Instrument(metrics.EntityTypeTrigger, conf.Name, func() ([]byte, error) {
		return t.Executor.ExecuteCommandWithStdin(conf.Command.Name, conf.Command.Arguments, ctx, stdin)
	})
	var result any = output
	text := string(output)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		text = PayloadResultInterrupted
		result = text
	case err != nil:
		text = PayloadResultFailedPrefix + err.Error()
		result = text
	}
	t.statusMu.Lock()
	t.publishTrigger(conf.Name, TopicSuffixResult, result)
	t.statusMu.Unlock()
	return text, err
}

func (t *Trigger) finish(conf config.Trigger, run *triggerRun) {
	defer run.cancel()
	t.statusMu.Lock()
	defer t.statusMu.Unlock()

	t.publishTrigger(conf.Name, TopicSuffixState, PayloadStatusStopped)

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

// publishTrigger is called with statusMu held, so a config swap cannot
// disable or move the MQTT route between resolving and publishing it.
func (t *Trigger) publishTrigger(name, suffix string, payload any) {
	t.lock.RLock()
	topic := t.triggersByName[name].Topic
	qos := t.publishQOS
	t.lock.RUnlock()

	if topic != "" {
		t.publish(topic+"/"+suffix, qos, payload)
	}
}

func (t *Trigger) Close(timeout time.Duration) error {
	t.lock.Lock()
	subscriptions := t.subscriptions
	t.subscriptions = nil
	t.generation++
	t.lock.Unlock()

	topics := make([]string, 0, len(subscriptions))
	for topic := range subscriptions {
		topics = append(topics, topic)
	}
	return unsubscribe(t.MqttClient, topics, timeout)
}

// Wait for the broker to remove the old subscriptions before a reload can
// register new handlers on the same topics.
func unsubscribe(client MQTT.Client, topics []string, timeout time.Duration) error {
	if client == nil || len(topics) == 0 || !client.IsConnectionOpen() {
		return nil
	}
	token := client.Unsubscribe(topics...)
	if !token.WaitTimeout(min(timeout, 5*time.Second)) {
		return errors.New("timed out waiting for MQTT unsubscription")
	}
	return token.Error()
}
