package mqtt

import (
	"context"
	"testing"
	"time"

	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/stretchr/testify/assert"
)

// TestSetQueryables ensures the queryable map can be swapped (hot reload):
// the old sensor disappears, the new one is queryable.
func TestSetQueryables(t *testing.T) {
	s := &SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: &fakePublisher{},
	}
	s.SetQueryables(map[string]config.GeneralSensor{
		"old": {Command: config.Command{Name: "/bin/echo", Arguments: []string{"a"}}, Interval: 1},
	})

	_, err := s.ExecuteOnce(context.Background(), "old")
	assert.NoError(t, err)

	s.SetQueryables(map[string]config.GeneralSensor{
		"new": {Command: config.Command{Name: "/bin/echo", Arguments: []string{"b"}}, Interval: 1},
	})

	_, err = s.ExecuteOnce(context.Background(), "old")
	assert.ErrorIs(t, err, ErrUnknownSensor)

	output, err := s.ExecuteOnce(context.Background(), "new")
	assert.NoError(t, err)
	assert.Equal(t, "b", string(output))
}

// TestSensorPublishOnChangeStable ensures publish_on_change skips publishing
// when the output does not change: many interval ticks, exactly one publish.
func TestSensorPublishOnChangeStable(t *testing.T) {
	pub := &fakePublisher{}
	s := &SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: pub,
	}

	s.Initialise(1, []config.GeneralSensor{{
		ResultTopic:     "tele/stable",
		Interval:        config.Interval(5 * time.Millisecond),
		PublishOnChange: true,
		Command:         config.Command{Name: "/bin/echo", Arguments: []string{"same"}},
	}})
	time.Sleep(150 * time.Millisecond) //~30 ticks
	assert.NoError(t, s.Close(time.Second))

	//count exact success payloads: a shutdown-cancelled in-flight tick may
	//additionally publish a <FAILED> result, which is not a "same" publish
	assert.Equal(t, 1, pub.count("tele/stable", "same"), "stable output must publish exactly once (the first run)")
}

// TestSensorPublishOnChangeChanges ensures a changing output is published on
// every run despite publish_on_change.
func TestSensorPublishOnChangeChanges(t *testing.T) {
	pub := &fakePublisher{}
	s := &SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: pub,
	}

	s.Initialise(1, []config.GeneralSensor{{
		ResultTopic:     "tele/random",
		Interval:        config.Interval(5 * time.Millisecond),
		PublishOnChange: true,
		Command:         config.Command{Name: "/bin/sh", Arguments: []string{"-c", "head -c 20 /dev/urandom | base64"}},
	}})
	time.Sleep(150 * time.Millisecond) //~30 ticks
	assert.NoError(t, s.Close(time.Second))

	assert.GreaterOrEqual(t, pub.countTopic("tele/random"), 10, "every changed output must be published")
}

// TestSensorDefaultPublishesAlways ensures the default (no publish_on_change)
// publishes on every tick as before.
func TestSensorDefaultPublishesAlways(t *testing.T) {
	pub := &fakePublisher{}
	s := &SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: pub,
	}

	s.Initialise(1, []config.GeneralSensor{{
		ResultTopic: "tele/stable",
		Interval:    config.Interval(5 * time.Millisecond),
		Command:     config.Command{Name: "/bin/echo", Arguments: []string{"same"}},
	}})
	time.Sleep(150 * time.Millisecond) //~30 ticks
	assert.NoError(t, s.Close(time.Second))

	assert.Greater(t, pub.countTopic("tele/stable"), 5, "default behaviour must publish on every tick")
}
