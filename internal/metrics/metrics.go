package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	CommandRuns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mqtt_executor_command_runs_total",
		Help: "Number of command executions per entity.",
	}, []string{"type", "name"})

	CommandErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mqtt_executor_command_errors_total",
		Help: "Number of command executions that did not complete successfully per entity (includes cancelled and timed-out runs).",
	}, []string{"type", "name"})

	CommandDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mqtt_executor_command_duration_seconds",
		Help:    "Duration of command executions per entity.",
		Buckets: prometheus.ExponentialBuckets(0.01, 10, 7), //10ms .. 100s
	}, []string{"type", "name"})
)

// EntityType* are the label values for the entity type label.
const (
	EntityTypeTrigger = "trigger"
	EntityTypeSensor  = "sensor"
)

// Instrument executes run while counting runs, errors and duration for the
// given entity (type + name labels).
func Instrument(entityType, name string, run func() ([]byte, error)) ([]byte, error) {
	CommandRuns.WithLabelValues(entityType, name).Inc()

	start := time.Now()
	out, err := run()

	CommandDuration.WithLabelValues(entityType, name).Observe(time.Since(start).Seconds())
	if err != nil {
		CommandErrors.WithLabelValues(entityType, name).Inc()
	}
	return out, err
}
