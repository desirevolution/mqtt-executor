package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

type StatusWorker struct {
	waitGroup  sync.WaitGroup
	cancelFunc context.CancelFunc

	Publisher transport.Publisher
}

func (s *StatusWorker) Initialise(availabilityConfigs config.Availability) {

	//generate a context so that we can cancel it later (see Close func)
	var ctx context.Context
	ctx, s.cancelFunc = context.WithCancel(context.Background())

	s.waitGroup.Add(1)
	go s.runStatus(ctx, availabilityConfigs)
}

func (s *StatusWorker) runStatus(ctx context.Context, availabilityConfig config.Availability) {
	defer s.waitGroup.Done()
	defer func() {
		//we should publish the last state (graceful shutdown should not trigger the mqtt-last-will!)
		//the MQTTPublisher waits for the token internally
		if err := s.Publisher.Publish(availabilityConfig.Topic, byte(1), true, availabilityConfig.Payload.Unavailable); err != nil {
			slog.Error("Failed to publish unavailable state", "topic", availabilityConfig.Topic, "error", err)
		}
	}()

	if err := s.Publisher.Publish(availabilityConfig.Topic, byte(1), true, availabilityConfig.Payload.Available); err != nil {
		slog.Error("Failed to publish available state", "topic", availabilityConfig.Topic, "error", err)
	}

	//wait until shutdown
	<-ctx.Done()
}

func (s *StatusWorker) Close(timeout time.Duration) error {
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
