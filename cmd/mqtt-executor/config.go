package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/denisbrodbeck/machineid"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	internalConf "github.com/rainu/mqtt-executor/internal/mqtt/config"
)

type applicationConfig struct {
	Broker       *string
	SubscribeQOS *int
	PublishQOS   *int
	Username     *string
	Password     *string
	ClientId     *string
	DeviceName   *string
	DeviceId     *string

	HomeassistantEnable *bool
	HomeassistantTopic  *string

	WebAddress *string

	LogFormat   *string
	ShowVersion *bool

	TopicConfigFile     *string
	TopicConfigurations internalConf.TopicConfigurations
}

var Config applicationConfig

func LoadConfig() {
	deviceId, _ := machineid.ID()
	if len(deviceId) > 8 {
		deviceId = deviceId[:8]
	}

	Config = applicationConfig{
		Broker:       flag.String("broker", "", "The broker URI. ex: tcp://127.0.0.1:1883"),
		SubscribeQOS: flag.Int("sub-qos", 1, "The Quality of Service for subscription 0,1,2 (default 1)"),
		PublishQOS:   flag.Int("pub-qos", 1, "The Quality of Service for publishing 0,1,2 (default 1)"),
		Username:     flag.String("user", "", "The User (optional)"),
		Password:     flag.String("password", "", "The password (optional)"),
		ClientId:     flag.String("client-id", "mqtt-executor", "The ClientID (optional)"),
		DeviceName:   flag.String("device-name", fmt.Sprintf("MQTTExecutor - %s", deviceId), "The name of this device (optional)"),
		DeviceId:     flag.String("device-id", deviceId, "A unique device id (optional)"),

		HomeassistantEnable: flag.Bool("home-assistant", false, "Enable home assistant support (optional)"),
		HomeassistantTopic:  flag.String("ha-discovery-prefix", "homeassistant/", "The mqtt topic prefix for homeassistant's discovery (optional)"),
		WebAddress:          flag.String("web", "127.0.0.1:8080", "The webhook server listen address (optional, empty disables the server)"),
		LogFormat:           flag.String("log-format", "text", "The log format: text or json (optional)"),
		ShowVersion:         flag.Bool("version", false, "Print the version and exit"),
		TopicConfigFile:     flag.String("config", "./config.yaml", "The topic configuration file"),
	}
	flag.Parse()

	if *Config.ShowVersion {
		fmt.Println(versionString())
		os.Exit(0)
	}
	if err := setupLogging(*Config.LogFormat, os.Stderr); err != nil {
		fatal(err.Error())
	}

	//flag validation
	if *Config.SubscribeQOS != 0 && *Config.SubscribeQOS != 1 && *Config.SubscribeQOS != 2 {
		fatal("Invalid qos level!")
	}
	if *Config.PublishQOS != 0 && *Config.PublishQOS != 1 && *Config.PublishQOS != 2 {
		fatal("Invalid qos level!")
	}
	if *Config.DeviceId == "" {
		fatal("Invalid device id!")
	}
	if *Config.TopicConfigFile == "" {
		fatal("Topic configuration file is missing!")
	}

	//load + validate the topic configuration (commands, mode rules) — the
	//same validation runs on hot reload (loadReloadConfig)
	configuration, err := loadReloadConfig()
	if err != nil {
		fatal(err.Error())
	}
	Config.TopicConfigurations = configuration

	//webhook-only mode: features without a publish target are disabled
	if *Config.Broker == "" {
		if *Config.HomeassistantEnable {
			slog.Warn("Homeassistant discovery requires a broker. Disabling home assistant support.")
		}
		if Config.TopicConfigurations.Availability != nil {
			slog.Warn("Availability topic configured but no broker set. Ignoring availability.")
		}
	}
}

// loadReloadConfig reads and validates the topic configuration for a hot
// reload without touching the running workers. On error the caller keeps the
// previous configuration.
func loadReloadConfig() (internalConf.TopicConfigurations, error) {
	configuration, err := internalConf.LoadTopicConfiguration(*Config.TopicConfigFile, *Config.DeviceId)
	if err != nil {
		return internalConf.TopicConfigurations{}, fmt.Errorf("could not read topic configuration: %w", err)
	}
	if err := configuration.ValidateCommands(); err != nil {
		return internalConf.TopicConfigurations{}, fmt.Errorf("invalid command: %w", err)
	}
	if err := validateBrokerRequirements(*Config.Broker, configuration); err != nil {
		return internalConf.TopicConfigurations{}, err
	}
	return configuration, nil
}

// validateBrokerRequirements enforces the mode rules: a broker is only
// mandatory when interval-driven sensors are configured (they need a publish
// target). Triggers work without a broker as well (webhook-only mode).
func validateBrokerRequirements(broker string, topicConfig internalConf.TopicConfigurations) error {
	if broker != "" {
		return nil
	}
	for _, sensor := range topicConfig.Sensors() {
		if sensor.Interval > 0 {
			return errors.New("broker is missing! interval-driven sensors require an mqtt broker to publish to")
		}
	}
	return nil
}

func (c *applicationConfig) GetMQTTOpts(
	onConn MQTT.OnConnectHandler,
	onLost MQTT.ConnectionLostHandler) *MQTT.ClientOptions {

	opts := MQTT.NewClientOptions()
	opts.SetWriteTimeout(5 * time.Second)

	opts.AddBroker(*c.Broker)
	if *c.Username != "" {
		opts.SetUsername(*c.Username)
	}
	if *c.Password != "" {
		opts.SetPassword(*c.Password)
	}
	if *c.ClientId != "" {
		opts.SetClientID(*c.ClientId)
	}

	if c.TopicConfigurations.Availability != nil {
		opts.WillEnabled = true
		opts.WillRetained = true
		opts.WillQos = byte(*c.PublishQOS)
		opts.WillPayload = []byte(c.TopicConfigurations.Availability.Payload.Unavailable)
		opts.WillTopic = c.TopicConfigurations.Availability.Topic
	}

	opts.SetOnConnectHandler(onConn)
	opts.SetConnectionLostHandler(onLost)

	return opts
}
