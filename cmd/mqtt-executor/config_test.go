package main

import (
	"os"
	"path/filepath"
	"testing"

	internalConf "github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadReloadConfig verifies the shared validation used by startup and hot
// reload (file must be valid, commands must exist, sensors need a broker).
func TestLoadReloadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
trigger:
  - name: t
    topic: cmnd/t
    command:
      name: /bin/echo
      arguments: [hi]
`), 0o644))

	//restore the global Config afterwards (LoadConfig tests may run after)
	previous := Config
	t.Cleanup(func() { Config = previous })

	broker := ""
	deviceId := "testdev"
	Config.TopicConfigFile = &path
	Config.Broker = &broker
	Config.DeviceId = &deviceId

	configuration, err := loadReloadConfig()
	require.NoError(t, err)
	assert.Len(t, configuration.Trigger, 1)

	//invalid command must fail
	require.NoError(t, os.WriteFile(path, []byte(`
trigger:
  - name: t
    topic: cmnd/t
    command:
      name: definitely-not-a-command-xyz
      arguments: []
`), 0o644))
	_, err = loadReloadConfig()
	assert.ErrorContains(t, err, "invalid command")

	//sensor without broker must fail
	require.NoError(t, os.WriteFile(path, []byte(`
sensor:
  - name: s
    topic: tele/s
    interval: 1s
    command:
      name: /bin/echo
      arguments: []
`), 0o644))
	_, err = loadReloadConfig()
	assert.ErrorContains(t, err, "broker is missing")
}

func TestValidateBrokerRequirements(t *testing.T) {
	withSensors := internalConf.TopicConfigurations{
		Sensor: []internalConf.Sensor{{
			GeneralSensor: internalConf.GeneralSensor{Interval: 1},
			Name:          "s",
		}},
	}
	withMultiSensors := internalConf.TopicConfigurations{
		MultiSensor: []internalConf.MultiSensor{{
			GeneralSensor: internalConf.GeneralSensor{Interval: 1},
		}},
	}
	withTrigger := internalConf.TopicConfigurations{
		Trigger: []internalConf.Trigger{{Name: "t", Topic: "cmnd/t"}},
	}

	tests := []struct {
		name        string
		broker      string
		topicConfig internalConf.TopicConfigurations
		expectError bool
	}{
		{name: "broker set", broker: "tcp://localhost:1883", topicConfig: withSensors, expectError: false},
		{name: "no broker, no entities", broker: "", topicConfig: internalConf.TopicConfigurations{}, expectError: false},
		{name: "no broker, triggers only (webhook-only mode)", broker: "", topicConfig: withTrigger, expectError: false},
		{name: "no broker, sensors", broker: "", topicConfig: withSensors, expectError: true},
		{name: "no broker, multi sensors", broker: "", topicConfig: withMultiSensors, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrokerRequirements(tt.broker, tt.topicConfig)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
