package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/metrics"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

type SensorWorker struct {
	waitGroup  sync.WaitGroup
	cancelFunc context.CancelFunc
	publishQOS byte

	Executor  *cmd.CommandExecutor
	Publisher transport.Publisher

	// queryables maps an entity name to its sensor configuration for manual
	// (webhook) queries. Single sensors register under their name, multi
	// sensor values under their value name (sharing the same configuration).
	// Swapped via SetQueryables (hot reload), read concurrently by ExecuteOnce.
	queryablesMu sync.RWMutex
	queryables   map[string]config.GeneralSensor
}

var ErrUnknownSensor = errors.New("unknown sensor")

// SetQueryables swaps the queryable sensor configurations (hot reload). It
// is safe to call while queries are served.
func (s *SensorWorker) SetQueryables(queryables map[string]config.GeneralSensor) {
	s.queryablesMu.Lock()
	defer s.queryablesMu.Unlock()
	s.queryables = queryables
}

func (s *SensorWorker) lookupQueryable(name string) (config.GeneralSensor, bool) {
	s.queryablesMu.RLock()
	defer s.queryablesMu.RUnlock()
	sensorConf, ok := s.queryables[name]
	return sensorConf, ok
}

func (s *SensorWorker) Initialise(publishQOS byte, sensorConfigs []config.GeneralSensor) {
	s.queryablesMu.Lock()
	s.publishQOS = publishQOS
	s.queryablesMu.Unlock()

	//generate a context so that we can cancel it later (see Close func)
	var ctx context.Context
	ctx, s.cancelFunc = context.WithCancel(context.Background())

	for _, sensorConf := range sensorConfigs {
		s.waitGroup.Add(1)
		go s.runSensor(ctx, sensorConf)
	}
}

func (s *SensorWorker) runSensor(ctx context.Context, sensorConf config.GeneralSensor) {
	defer s.waitGroup.Done()

	var lastPublished string
	hasPublished := false

	run := func() {
		output, execErr := s.executeSensor(ctx, sensorConf)

		//the comparison basis is the last PUBLISHED payload (success output
		//or <FAILED> error): a recovery after an intermittent error differs
		//from the failed payload and is published again
		payload := output
		if execErr != nil {
			payload = []byte(PayloadResultFailedPrefix + execErr.Error())
		}

		//with publish_on_change: skip publishing when the payload did not
		//change since the last run (manual webhook queries always publish,
		//see ExecuteOnce). Without the option the state is not tracked at all.
		if sensorConf.PublishOnChange && hasPublished && string(payload) == lastPublished {
			return
		}
		if sensorConf.PublishOnChange {
			lastPublished, hasPublished = string(payload), true
		}

		//errors are published just like regular results
		s.publishResult(sensorConf.ResultTopic, s.publishQOS, sensorConf.Retained, payload)
	}

	//first execution
	run()

	ticker := time.NewTicker(time.Duration(sensorConf.Interval))
	defer ticker.Stop()
	for {
		//wait until next tick or shutdown
		select {
		case <-ticker.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

// qos returns the current publish QoS (lock-guarded: interval goroutines and
// webhook queries read it while a hot reload may swap it).
func (s *SensorWorker) qos() byte {
	s.queryablesMu.RLock()
	defer s.queryablesMu.RUnlock()
	return s.publishQOS
}

// executeSensor runs the sensor's command (with the optional per-sensor
// timeout applied to the given context) and returns the output without
// publishing. Publishing is decided by the callers (interval runs respect
// publish_on_change, manual queries always publish).
func (s *SensorWorker) executeSensor(ctx context.Context, sensorConf config.GeneralSensor) ([]byte, error) {
	if sensorConf.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(sensorConf.Timeout))
		defer cancel()
	}

	return metrics.Instrument(metrics.EntityTypeSensor, sensorConf.Name, func() ([]byte, error) {
		return s.Executor.ExecuteCommandWithContext(sensorConf.Command.Name, sensorConf.Command.Arguments, ctx)
	})
}

// ExecuteOnce runs the named sensor's command synchronously and always
// publishes the result (an explicit query counts more than chatter
// reduction). It returns the command output and the execution error, if any.
// The given context cancels the command (e.g. when the HTTP request is
// aborted or on shutdown).
func (s *SensorWorker) ExecuteOnce(ctx context.Context, name string) ([]byte, error) {
	sensorConf, ok := s.lookupQueryable(name)
	if !ok {
		return nil, ErrUnknownSensor
	}

	output, execErr := s.executeSensor(ctx, sensorConf)
	if execErr != nil {
		s.publishResult(sensorConf.ResultTopic, s.publishQOS, sensorConf.Retained, PayloadResultFailedPrefix+execErr.Error())
		return output, execErr
	}

	s.publishResult(sensorConf.ResultTopic, s.publishQOS, sensorConf.Retained, output)
	return output, nil
}

func (s *SensorWorker) publishResult(topic string, publishQOS byte, retained bool, payload any) {
	if err := s.Publisher.Publish(topic, publishQOS, retained, payload); err != nil {
		slog.Error("Failed to publish sensor result", "topic", topic, "error", err)
	}
}

func (s *SensorWorker) Close(timeout time.Duration) error {
	if s.cancelFunc != nil {
		//close the context to interrupt possible running commands
		s.cancelFunc()
	}

	done := make(chan struct{})
	go func() {
		s.waitGroup.Wait()
		close(done)
	}()

	//wait for WaitGroup or Timeout
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout exceeded")
	}
}
