package mqtt

import (
	"log/slog"

	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

type StatusWorker struct {
	Availability *config.Availability
	Publisher    transport.Publisher
}

func (s *StatusWorker) Online() {
	if s.Availability != nil {
		s.publish(s.Availability.Payload.Available)
	}
}

func (s *StatusWorker) Offline() {
	if s.Availability != nil {
		s.publish(s.Availability.Payload.Unavailable)
	}
}

func (s *StatusWorker) publish(payload string) {
	if err := s.Publisher.Publish(s.Availability.Topic, 1, true, payload); err != nil {
		slog.Error("Failed to publish MQTT availability", "topic", s.Availability.Topic, "error", err)
	}
}
