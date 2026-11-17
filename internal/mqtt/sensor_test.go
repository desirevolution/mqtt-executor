package mqtt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitialiseSensorQueryables ensures the queryable map can be swapped (hot reload):
// the old sensor disappears, the new one is queryable.
func TestInitialiseSensorQueryables(t *testing.T) {
	s := &SensorWorker{
		Executor:  cmd.NewCommandExecutor(),
		Publisher: &fakePublisher{},
	}
	s.Initialise(1, []config.GeneralSensor{{Name: "old", Command: config.Command{Name: "/bin/echo", Arguments: []string{"a"}}}})

	_, err := s.ExecuteOnce(context.Background(), "old")
	assert.NoError(t, err)

	assert.NoError(t, s.Close(time.Second))
	s.Initialise(1, []config.GeneralSensor{{Name: "new", Command: config.Command{Name: "/bin/echo", Arguments: []string{"b"}}}})
	t.Cleanup(func() { assert.NoError(t, s.Close(time.Second)) })

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

func TestManualRequestReusesActiveMeasurementAndForcesPublish(t *testing.T) {
	pub := &fakePublisher{}
	worker := &SensorWorker{Executor: cmd.NewCommandExecutor(), Publisher: pub}
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	worker.Initialise(1, []config.GeneralSensor{{
		Name: "volume", Aliases: []string{"volume_alias"}, ResultTopic: "tele/volume",
		PublishOnChange: true,
		Command: config.Command{Name: "/bin/sh", Arguments: []string{
			"-c", `echo run >> "$1"; while [ ! -f "$2" ]; do sleep 0.01; done; echo 42`,
			"sensor", started, release,
		}},
	}})
	t.Cleanup(func() { assert.NoError(t, worker.Close(time.Second)) })

	session := worker.currentSession()
	sensor := session.queryables["volume"]
	require.Same(t, sensor, session.queryables["volume_alias"])
	sensor.lastPublished, sensor.hasPublished = "42", true
	periodic := worker.measure(session, sensor, false)
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, time.Second, time.Millisecond)
	manual := worker.measure(session, sensor, true)
	require.Same(t, periodic, manual)
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	select {
	case <-manual.done:
	case <-time.After(time.Second):
		t.Fatal("measurement did not finish")
	}
	data, err := os.ReadFile(started)
	require.NoError(t, err)
	assert.Equal(t, "run\n", string(data))
	assert.Equal(t, "42", string(manual.output))
	assert.NoError(t, manual.err)
	assert.Equal(t, 1, pub.count("tele/volume", "42"))
}
