package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/metrics"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

var ErrUnknownSensor = errors.New("unknown sensor")

type SensorWorker struct {
	mu      sync.RWMutex
	session *sensorSession

	Executor   *cmd.CommandExecutor
	Publisher  transport.Publisher
	MqttClient MQTT.Client
}

type sensorSession struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex // coordinates admissions with shutdown and protects measurements
	workers    sync.WaitGroup
	publishQOS byte
	queryables map[string]*configuredSensor
	topics     map[string]*configuredSensor
}

type configuredSensor struct {
	config        config.GeneralSensor
	active        *measurement
	lastPublished string
	hasPublished  bool
}

type measurement struct {
	done   chan struct{}
	manual bool
	output []byte
	err    error
}

func (s *SensorWorker) Initialise(publishQOS byte, configs []config.GeneralSensor) {
	ctx, cancel := context.WithCancel(context.Background())
	session := &sensorSession{
		ctx: ctx, cancel: cancel, publishQOS: publishQOS,
		queryables: make(map[string]*configuredSensor),
		topics:     make(map[string]*configuredSensor),
	}
	for _, conf := range configs {
		sensor := &configuredSensor{config: conf}
		session.queryables[conf.Name] = sensor
		for _, alias := range conf.Aliases {
			session.queryables[alias] = sensor
		}
		if conf.QueryTopic != "" {
			session.topics[conf.QueryTopic] = sensor
		}
		if conf.Interval > 0 {
			session.workers.Go(func() { s.runSensor(session, sensor) })
		}
	}

	s.mu.Lock()
	s.session = session
	s.mu.Unlock()
}

func (s *SensorWorker) ReInitialise(subscribeQOS byte) {
	session := s.currentSession()
	if s.MqttClient == nil || session == nil {
		return
	}
	for topic, sensor := range session.topics {
		s.MqttClient.Subscribe(topic, subscribeQOS, func(_ MQTT.Client, message MQTT.Message) {
			if !message.Retained() {
				s.measure(session, sensor, true)
			}
		})
	}
}

func (s *SensorWorker) currentSession() *sensorSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.session
}

func (s *SensorWorker) runSensor(session *sensorSession, sensor *configuredSensor) {
	ticker := time.NewTicker(time.Duration(sensor.config.Interval))
	defer ticker.Stop()
	for {
		run := s.measure(session, sensor, false)
		if run == nil {
			return
		}
		select {
		case <-session.ctx.Done():
			return
		case <-run.done:
		}
		select {
		case <-session.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// measure joins an active measurement or starts one shared by every ingress.
// The lifetime belongs to the worker, so an HTTP disconnect only cancels
// that caller's wait. Reload and shutdown cancel the measurement itself.
func (s *SensorWorker) measure(session *sensorSession, sensor *configuredSensor, manual bool) *measurement {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.ctx.Err() != nil {
		return nil
	}
	if run := sensor.active; run != nil {
		run.manual = run.manual || manual
		return run
	}
	run := &measurement{done: make(chan struct{}), manual: manual}
	sensor.active = run
	session.workers.Go(func() { s.completeMeasurement(session, sensor, run) })
	return run
}

func (s *SensorWorker) completeMeasurement(session *sensorSession, sensor *configuredSensor, run *measurement) {
	conf := sensor.config
	ctx := session.ctx
	if conf.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(conf.Timeout))
		defer cancel()
	}
	run.output, run.err = metrics.Instrument(metrics.EntityTypeSensor, conf.Name, func() ([]byte, error) {
		return s.Executor.ExecuteCommandWithContext(conf.Command.Name, conf.Command.Arguments, ctx)
	})
	payload := run.output
	if run.err != nil {
		payload = []byte(PayloadResultFailedPrefix + run.err.Error())
	}

	session.mu.Lock()
	publish := session.ctx.Err() == nil && conf.ResultTopic != "" &&
		(run.manual || !conf.PublishOnChange || !sensor.hasPublished || string(payload) != sensor.lastPublished)
	if publish {
		// Keep the measurement joinable until its publication finishes.
		session.mu.Unlock()
		err := s.Publisher.Publish(conf.ResultTopic, session.publishQOS, conf.Retained, payload)
		session.mu.Lock()
		if err == nil {
			sensor.lastPublished, sensor.hasPublished = string(payload), true
		} else {
			slog.Error("Failed to publish sensor result", "topic", conf.ResultTopic, "error", err)
		}
	}
	sensor.active = nil
	close(run.done)
	session.mu.Unlock()
}

func (s *SensorWorker) ExecuteOnce(ctx context.Context, name string) ([]byte, error) {
	session := s.currentSession()
	if session == nil || session.queryables[name] == nil {
		return nil, ErrUnknownSensor
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run := s.measure(session, session.queryables[name], true)
	if run == nil {
		return nil, context.Canceled
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-run.done:
		return run.output, run.err
	}
}

func (s *SensorWorker) Close(timeout time.Duration) error {
	s.mu.Lock()
	session := s.session
	s.session = nil
	s.mu.Unlock()
	if session == nil {
		return nil
	}

	session.mu.Lock()
	session.cancel()
	session.mu.Unlock()
	if s.MqttClient != nil {
		for topic := range session.topics {
			s.MqttClient.Unsubscribe(topic)
		}
	}

	done := make(chan struct{})
	go func() {
		session.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout exceeded")
	}
}
