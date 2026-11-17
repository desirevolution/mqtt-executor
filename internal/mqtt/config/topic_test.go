package config

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTopicConfiguration(t *testing.T) {
	deviceId := "D3V1C3"
	tests := []struct {
		name              string
		content           string
		expectedResult    TopicConfigurations
		expectedError     string
		expectedErrorPart string
	}{
		{
			name:          "empty config",
			expectedError: "could not read topic configuration file: EOF",
		},
		{
			name:    "empty yaml",
			content: `{}`,
		},
		{
			name: "Availability",
			content: `
availability:
  topic: tele/__DEVICE_ID__/status
  payload:
    available: ON
    unavailable: OFF
`,
			expectedResult: TopicConfigurations{
				Availability: &Availability{
					Topic: fmt.Sprintf("tele/%s/status", deviceId),
					Payload: availabilityPayload{
						Available:   "ON",
						Unavailable: "OFF",
					},
				},
			},
		},
		{
			name: "Availability default values",
			content: `
availability:
  topic: tele/__DEVICE_ID__/status
`,
			expectedResult: TopicConfigurations{
				Availability: &Availability{
					Topic: fmt.Sprintf("tele/%s/status", deviceId),
					Payload: availabilityPayload{
						Available:   "Online",
						Unavailable: "Offline",
					},
				},
			},
		},
		{
			name:          "Availability invalid topic",
			content:       `{"availability": {"topic": "tele/+/status"}}`,
			expectedError: "invalid config: invalid availability topic: invalid character",
		},
		{
			name:          "Availability empty topic",
			content:       `{"availability": {"topic": ""}}`,
			expectedError: "invalid config: invalid availability topic: must not be empty",
		},
		{
			name: "Sensor",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/__DEVICE_ID__/status
    unit: kWh
    interval: "13s"
    icon: hassio-icon
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Sensor: []Sensor{{
					GeneralSensor: GeneralSensor{
						ResultTopic: fmt.Sprintf("tele/%s/status", deviceId),
						Interval:    *interval(13 * time.Second),
						Name:        "My sweat sensor",
						Command: Command{
							Name:      "/usr/bin/bash",
							Arguments: []string{"echo"},
						},
					},
					Name: "My sweat sensor",
					Unit: "kWh",
					Icon: "hassio-icon",
				}},
			},
		},
		{
			name: "Sensor unquoted interval",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/__DEVICE_ID__/status
    interval: 13s
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Sensor: []Sensor{{
					GeneralSensor: GeneralSensor{
						ResultTopic: fmt.Sprintf("tele/%s/status", deviceId),
						Interval:    *interval(13 * time.Second),
						Name:        "My sweat sensor",
						Command: Command{
							Name:      "/usr/bin/bash",
							Arguments: []string{"echo"},
						},
					},
					Name: "My sweat sensor",
				}},
			},
		},
		{
			name: "On-demand sensor without interval",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/__DEVICE_ID__/status
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Sensor: []Sensor{{
					Name: "My sweat sensor",
					GeneralSensor: GeneralSensor{
						Name: "My sweat sensor", ResultTopic: fmt.Sprintf("tele/%s/status", deviceId),
						Command: Command{Name: "/usr/bin/bash", Arguments: []string{"echo"}},
					},
				}},
			},
		},
		{
			name: "Sensor missing name",
			content: `
sensor:
  - topic: tele/__DEVICE_ID__/status
    interval: "13s"
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#0): name must not be empty",
		},
		{
			name: "Sensor invalid topic",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/+/status
    interval: "13s"
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#0): invalid topic: invalid character",
		},
		{
			name: "Sensor empty topic",
			content: `
sensor:
  - name: My sweat sensor
    interval: "13s"
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#0): invalid topic: must not be empty",
		},
		{
			name: "Sensor empty command",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/status
    interval: "13s"
    command:
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#0): command name must not be empty",
		},
		{
			name: "Sensor duplicate name",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/status
    interval: "13s"
    command:
      name: /usr/bin/bash
      arguments: [echo]
  - name: My sweat sensor
    topic: tele/status
    interval: "13s"
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#1): sensor with this name already exists",
		},
		{
			name: "Trigger",
			content: `
trigger:
  - name: My sweat sensor
    topic: tele/__DEVICE_ID__/status
    icon: hassio-icon
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Trigger: []Trigger{{
					Name:  "My sweat sensor",
					Topic: fmt.Sprintf("tele/%s/status", deviceId),
					Icon:  "hassio-icon",
					Command: Command{
						Name:      "/usr/bin/bash",
						Arguments: []string{"echo"},
					},
				}},
			},
		},
		{
			name: "Trigger missing name",
			content: `
trigger:
  - topic: tele/__DEVICE_ID__/status
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid trigger (#0): name must not be empty",
		},
		{
			name: "Trigger invalid topic",
			content: `
trigger:
  - name: My sweat sensor
    topic: tele/+/status
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid trigger (#0): invalid topic: invalid character",
		},
		{
			name: "Disabled trigger without topic or webhook ID",
			content: `
trigger:
  - name: My sweat sensor
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Trigger: []Trigger{{
					Name:    "My sweat sensor",
					Command: Command{Name: "/usr/bin/bash", Arguments: []string{"echo"}},
				}},
			},
		},
		{
			name: "Trigger empty command",
			content: `
trigger:
  - name: My sweat sensor
    topic: tele/status
    command:
      arguments: [echo]
`,
			expectedError: "invalid config: invalid trigger (#0): command name must not be empty",
		},
		{
			name: "Trigger duplicate name",
			content: `
trigger:
  - name: My sweat sensor
    topic: tele/status
    command:
      name: /usr/bin/bash
      arguments: [echo]
  - name: My sweat sensor
    topic: tele/status
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid trigger (#1): trigger with this name already exists",
		},
		{
			//strict parsing (A1): unknown keys must be rejected
			name: "Unknown key",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/status
    interval: "13s"
    freshness: definitely
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedErrorPart: "field freshness not found in type config.Sensor",
		},
		{
			name:              "Invalid yaml syntax",
			content:           "sensor: [unclosed",
			expectedErrorPart: "could not read topic configuration file: yaml:",
		},
		{
			name: "Sensor with timeout",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/__DEVICE_ID__/status
    interval: "13s"
    timeout: 30s
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Sensor: []Sensor{{
					GeneralSensor: GeneralSensor{
						ResultTopic: fmt.Sprintf("tele/%s/status", deviceId),
						Interval:    *interval(13 * time.Second),
						Timeout:     *interval(30 * time.Second),
						Name:        "My sweat sensor",
						Command: Command{
							Name:      "/usr/bin/bash",
							Arguments: []string{"echo"},
						},
					},
					Name: "My sweat sensor",
				}},
			},
		},
		{
			name: "Sensor negative timeout",
			content: `
sensor:
  - name: My sweat sensor
    topic: tele/status
    interval: "13s"
    timeout: -5s
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid sensor (#0): timeout must not be negative",
		},
		{
			name: "Trigger with timeout",
			content: `
trigger:
  - name: My sweat sensor
    topic: tele/status
    timeout: 1m30s
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedResult: TopicConfigurations{
				Trigger: []Trigger{{
					Name:    "My sweat sensor",
					Topic:   "tele/status",
					Timeout: *interval(90 * time.Second),
					Command: Command{
						Name:      "/usr/bin/bash",
						Arguments: []string{"echo"},
					},
				}},
			},
		},
		{
			name: "Multi sensor without values",
			content: `
multi_sensor:
  - topic: tele/status
    interval: "13s"
    values: []
    command:
      name: /usr/bin/bash
      arguments: [echo]
`,
			expectedError: "invalid config: invalid multi sensor (#0): values must not be empty",
		},
		{
			name:              "Duplicate key",
			content:           "sensor: []\nsensor: []\n",
			expectedErrorPart: "already defined",
		},
	}

	for _, tt := range tests {
		t.Run("TestLoadTopicConfiguration_"+tt.name, func(t *testing.T) {
			tmpFile := testFile(tt.content)
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			configuration, err := LoadTopicConfiguration(tmpFile.Name(), deviceId)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
			} else if tt.expectedErrorPart != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorPart)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, configuration)
			}

		})
	}
}

func testFile(content string) *os.File {
	file, err := os.CreateTemp("", "TestLoadTopicConfiguration")
	if err != nil {
		panic(err)
	}

	_, err = file.WriteString(content)
	if err != nil {
		panic(err)
	}

	return file
}

func interval(d time.Duration) *Interval {
	i := Interval(d)
	return &i
}

func TestTriggerWebhookIDsAndDormantRunTopic(t *testing.T) {
	conf := TopicConfigurations{Trigger: []Trigger{
		{Name: "MQTT only", Topic: "cmd/one", RunTopic: "cmd/run", Command: Command{Name: "/bin/echo"}},
		{Name: "Webhook only", WebhookID: "two-2", RunTopic: "cmd/run", Command: Command{Name: "/bin/echo"}},
		{Name: "Disabled", RunTopic: "cmd/run", Command: Command{Name: "intentionally-unavailable-binary"}},
	}}
	require.NoError(t, conf.validate())
	require.NoError(t, conf.validateRequestTopics())
	require.NoError(t, conf.ValidateCommands())

	conf.Trigger[0].WebhookID = "two-2"
	require.ErrorContains(t, conf.validate(), "duplicate webhook_id")
	conf.Trigger[0].WebhookID = "Upper/Two"
	require.ErrorContains(t, conf.validate(), "invalid webhook_id")
}
