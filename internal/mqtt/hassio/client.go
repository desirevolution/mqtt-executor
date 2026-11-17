package hassio

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/rainu/mqtt-executor/internal/mqtt"
	"github.com/rainu/mqtt-executor/internal/mqtt/config"
	"github.com/rainu/mqtt-executor/internal/transport"
)

type generalConfig struct {
	Name                string `json:"name"`
	AvailabilityTopic   string `json:"avty_t,omitempty"`
	PayloadAvailable    string `json:"pl_avail,omitempty"`
	PayloadNotAvailable string `json:"pl_not_avail,omitempty"`
	UniqueId            string `json:"uniq_id"`
	Icon                string `json:"ic,omitempty"`
	Device              device `json:"dev,omitempty"`
}

type sensorConfig struct {
	generalConfig

	StateTopic      string `json:"stat_t"`
	MeasurementUnit string `json:"unit_of_meas,omitempty"`
	ValueTemplate   string `json:"value_template,omitempty"`
	ForceUpdate     *bool  `json:"frc_upd,omitempty"`
}

type triggerConfig struct {
	generalConfig

	CommandTopic string `json:"cmd_t"`
	StateTopic   string `json:"stat_t"`
	PayloadStart string `json:"pl_on"`
	PayloadStop  string `json:"pl_off"`
	StateRunning string `json:"stat_on"`
	StateStopped string `json:"stat_off"`
}

type device struct {
	Name         string   `json:"name,omitempty"`
	Ids          []string `json:"ids"`
	Model        string   `json:"mdl,omitempty"`
	Manufacturer string   `json:"mf,omitempty"`
	Version      string   `json:"sw,omitempty"`
}

type AvailabilityConfig struct {
	Topic               string
	AvailablePayload    string
	NotAvailablePayload string
}

type Client struct {
	DeviceName  string
	DeviceId    string
	TopicPrefix string
	Publisher   transport.Publisher
}

func (c *Client) PublishDiscoveryConfig(config config.TopicConfigurations) error {
	slog.Info("Initialise homeassistant config.")

	//status
	if config.Availability != nil {
		targetTopic := fmt.Sprintf("%ssensor/%s_status/config", c.TopicPrefix, c.DeviceId)
		payload, err := c.generatePayloadForStatus(config.Availability)
		if err != nil {
			return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
		}
		if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
			return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
		}
	}

	//sensor
	for _, sensor := range config.Sensor {
		if sensor.ResultTopic == "" {
			continue
		}
		targetTopic := fmt.Sprintf("%ssensor/%s_%s/config", c.TopicPrefix, c.DeviceId, friendlyName(sensor.Name))
		payload, err := c.generatePayloadForSensor(config.Availability, sensor)
		if err != nil {
			return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
		}
		if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
			return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
		}
	}

	//multi sensor
	for _, sensor := range config.MultiSensor {
		if sensor.ResultTopic == "" {
			continue
		}
		for _, sensorValue := range sensor.Values {
			targetTopic := fmt.Sprintf("%ssensor/%s_%s/config", c.TopicPrefix, c.DeviceId, friendlyName(sensorValue.Name))
			payload, err := c.generatePayloadForMultiSensor(config.Availability, sensor, sensorValue)
			if err != nil {
				return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
			}
			if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
				return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
			}
		}
	}

	//trigger
	for _, trigger := range config.Trigger {
		targetTopic := fmt.Sprintf("%sswitch/%s/%s/config", c.TopicPrefix, c.DeviceId, friendlyName(trigger.Name))
		payload, err := c.generateSwitchPayloadForTriggerAction(config.Availability, trigger)
		if err != nil {
			return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
		}
		if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
			return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
		}

		//publish the trigger-result as sensor data
		targetTopic = fmt.Sprintf("%ssensor/%s_%s/result/config", c.TopicPrefix, c.DeviceId, friendlyName(trigger.Name))
		payload, err = c.generateResultPayloadForTriggerAction(config.Availability, trigger)
		if err != nil {
			return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
		}
		if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
			return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
		}

		//publish the trigger-state as sensor data
		targetTopic = fmt.Sprintf("%ssensor/%s_%s/state/config", c.TopicPrefix, c.DeviceId, friendlyName(trigger.Name))
		payload, err = c.generateStatePayloadForTriggerAction(config.Availability, trigger)
		if err != nil {
			return fmt.Errorf("discovery config for %q: %w", targetTopic, err)
		}
		if err := c.Publisher.Publish(targetTopic, byte(1), false, payload); err != nil {
			return fmt.Errorf("publish discovery config %q: %w", targetTopic, err)
		}
	}
	return nil
}

func friendlyName(name string) string {
	return strings.ReplaceAll(name, " ", "_")
}

func (c *Client) buildDevice() device {
	return device{
		Name:         c.DeviceName,
		Ids:          []string{c.DeviceId},
		Manufacturer: "rainu",
		Model:        runtime.GOOS,
		Version:      "mqtt-executor",
	}
}

func (c *Client) generatePayloadForStatus(availability *config.Availability) ([]byte, error) {
	conf := sensorConfig{
		generalConfig: generalConfig{
			Name:                "Status",
			PayloadAvailable:    availability.Payload.Available,
			PayloadNotAvailable: availability.Payload.Unavailable,
			UniqueId:            fmt.Sprintf("%s_status", c.DeviceId),
			Device:              c.buildDevice(),
		},
		StateTopic: availability.Topic,
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		conf.Device.Version += " " + info.Main.Version
	}

	return json.Marshal(conf)
}

func (c *Client) generatePayloadForSensor(availability *config.Availability, sensor config.Sensor) ([]byte, error) {
	bTrue := true
	conf := sensorConfig{
		generalConfig: generalConfig{
			Name:     sensor.Name,
			Icon:     sensor.Icon,
			UniqueId: fmt.Sprintf("%s_%s", c.DeviceId, friendlyName(sensor.Name)),
			Device:   c.buildDevice(),
		},
		StateTopic:      sensor.ResultTopic,
		MeasurementUnit: sensor.Unit,
		ForceUpdate:     &bTrue,
	}
	addAvailability(&conf.generalConfig, availability)

	return json.Marshal(conf)
}

func (c *Client) generatePayloadForMultiSensor(availability *config.Availability, sensor config.MultiSensor, sensorValue config.MultiSensorValue) ([]byte, error) {
	bTrue := true
	conf := sensorConfig{
		generalConfig: generalConfig{
			Name:     sensorValue.Name,
			Icon:     sensorValue.Icon,
			UniqueId: fmt.Sprintf("%s_%s", c.DeviceId, friendlyName(sensorValue.Name)),
			Device:   c.buildDevice(),
		},
		StateTopic:      sensor.ResultTopic,
		ValueTemplate:   sensorValue.Template,
		MeasurementUnit: sensorValue.Unit,
		ForceUpdate:     &bTrue,
	}
	addAvailability(&conf.generalConfig, availability)

	return json.Marshal(conf)
}

func (c *Client) generateSwitchPayloadForTriggerAction(availability *config.Availability, trigger config.Trigger) ([]byte, error) {
	conf := triggerConfig{
		generalConfig: generalConfig{
			Name:     fmt.Sprintf("%s", trigger.Name),
			Icon:     trigger.Icon,
			UniqueId: fmt.Sprintf("%s_%s", c.DeviceId, friendlyName(trigger.Name)),
			Device:   c.buildDevice(),
		},
		CommandTopic: trigger.Topic,
		PayloadStart: mqtt.PayloadStart,
		PayloadStop:  mqtt.PayloadStop,
		StateTopic:   fmt.Sprintf("%s/%s", trigger.Topic, mqtt.TopicSuffixState),
		StateRunning: mqtt.PayloadStatusRunning,
		StateStopped: mqtt.PayloadStatusStopped,
	}
	addAvailability(&conf.generalConfig, availability)

	return json.Marshal(conf)
}

func (c *Client) generateResultPayloadForTriggerAction(availability *config.Availability, trigger config.Trigger) ([]byte, error) {
	conf := sensorConfig{
		generalConfig: generalConfig{
			Name:     fmt.Sprintf("%s - Result", trigger.Name),
			Icon:     trigger.Icon,
			UniqueId: fmt.Sprintf("%s_%s_result", c.DeviceId, friendlyName(trigger.Name)),
			Device:   c.buildDevice(),
		},
		StateTopic: fmt.Sprintf("%s/%s", trigger.Topic, mqtt.TopicSuffixResult),
	}
	addAvailability(&conf.generalConfig, availability)

	return json.Marshal(conf)
}

func (c *Client) generateStatePayloadForTriggerAction(availability *config.Availability, trigger config.Trigger) ([]byte, error) {
	conf := sensorConfig{
		generalConfig: generalConfig{
			Name:     fmt.Sprintf("%s - State", trigger.Name),
			Icon:     trigger.Icon,
			UniqueId: fmt.Sprintf("%s_%s_state", c.DeviceId, friendlyName(trigger.Name)),
			Device:   c.buildDevice(),
		},
		StateTopic: fmt.Sprintf("%s/%s", trigger.Topic, mqtt.TopicSuffixState),
	}
	addAvailability(&conf.generalConfig, availability)

	return json.Marshal(conf)
}

func addAvailability(config *generalConfig, availability *config.Availability) {
	if availability != nil {
		config.AvailabilityTopic = availability.Topic
		config.PayloadAvailable = availability.Payload.Available
		config.PayloadNotAvailable = availability.Payload.Unavailable
	}
}
