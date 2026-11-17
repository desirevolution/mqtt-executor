package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type TopicConfigurations struct {
	Availability *Availability `yaml:"availability,omitempty"`
	Trigger      []Trigger     `yaml:"trigger"`
	Sensor       []Sensor      `yaml:"sensor"`
	MultiSensor  []MultiSensor `yaml:"multi_sensor"`
}

type Availability struct {
	Topic   string              `yaml:"topic"`
	Payload availabilityPayload `yaml:"payload"`
}
type availabilityPayload struct {
	Available   string `yaml:"available"`
	Unavailable string `yaml:"unavailable"`
}

type Trigger struct {
	Name      string  `yaml:"name"`
	Topic     string  `yaml:"topic"`
	RunTopic  string  `yaml:"run_topic"`
	WebhookID string  `yaml:"webhook_id"`
	Icon      string  `yaml:"icon"`
	Command   Command `yaml:"command"`
	// StdinPayload: pass the incoming payload (MQTT message body / webhook
	// request body) to the command as stdin
	StdinPayload bool `yaml:"stdin_payload"`
	// Timeout: interrupt the command after this duration (optional, default:
	// run until completion)
	Timeout Interval `yaml:"timeout"`
}

type GeneralSensor struct {
	ResultTopic string   `yaml:"topic"`
	QueryTopic  string   `yaml:"query_topic"`
	Retained    bool     `yaml:"retained"`
	Interval    Interval `yaml:"interval"`
	Command     Command  `yaml:"command"`
	// Timeout: interrupt the command after this duration (optional, default:
	// run until completion)
	Timeout Interval `yaml:"timeout"`
	// PublishOnChange: skip publishing the result when the output equals the
	// previous run's output (optional, default: always publish)
	PublishOnChange bool `yaml:"publish_on_change"`
	// Name identifies the sensor for metrics/logs; not part of the YAML
	// config (populated by LoadTopicConfiguration)
	Name    string   `yaml:"-"`
	Aliases []string `yaml:"-"`
}

type Sensor struct {
	//explicit inline tag: yaml's KnownFields(true) (strict parsing) does not
	//recurse into untagged embedded structs
	GeneralSensor `yaml:",inline"`

	Name string `yaml:"name"`
	Unit string `yaml:"unit"`
	Icon string `yaml:"icon"`
}

type MultiSensor struct {
	GeneralSensor `yaml:",inline"`

	Values []MultiSensorValue `yaml:"values"`
}

type MultiSensorValue struct {
	Name     string `yaml:"name"`
	Template string `yaml:"template"`
	Unit     string `yaml:"unit"`
	Icon     string `yaml:"icon"`
}

type Command struct {
	Name      string   `yaml:"name"`
	Arguments []string `yaml:"arguments"`
}

func LoadTopicConfiguration(configFilePath, deviceId string) (TopicConfigurations, error) {
	configFile, err := os.Open(configFilePath)
	if err != nil {
		return TopicConfigurations{}, fmt.Errorf("error while opening topic configuration file: %w", err)
	}
	defer configFile.Close()

	var topicConfig TopicConfigurations
	//strict parsing: unknown keys cause an error instead of being silently
	//ignored (catches typos in the config file)
	decoder := yaml.NewDecoder(configFile)
	decoder.KnownFields(true)
	err = decoder.Decode(&topicConfig)
	if err != nil {
		return TopicConfigurations{}, fmt.Errorf("could not read topic configuration file: %w", err)
	}

	err = topicConfig.validate()
	if err != nil {
		return TopicConfigurations{}, fmt.Errorf("invalid config: %w", err)
	}

	//replace DEVICE_ID
	if topicConfig.Availability != nil {
		topicConfig.Availability.Topic = strings.ReplaceAll(topicConfig.Availability.Topic, "__DEVICE_ID__", deviceId)
		if topicConfig.Availability.Payload.Available == "" {
			topicConfig.Availability.Payload.Available = "Online"
		}
		if topicConfig.Availability.Payload.Unavailable == "" {
			topicConfig.Availability.Payload.Unavailable = "Offline"
		}
	}
	for i := range topicConfig.Trigger {
		topicConfig.Trigger[i].Topic = strings.ReplaceAll(topicConfig.Trigger[i].Topic, "__DEVICE_ID__", deviceId)
		topicConfig.Trigger[i].RunTopic = strings.ReplaceAll(topicConfig.Trigger[i].RunTopic, "__DEVICE_ID__", deviceId)
	}
	for i := range topicConfig.Sensor {
		topicConfig.Sensor[i].ResultTopic = strings.ReplaceAll(topicConfig.Sensor[i].ResultTopic, "__DEVICE_ID__", deviceId)
		topicConfig.Sensor[i].QueryTopic = strings.ReplaceAll(topicConfig.Sensor[i].QueryTopic, "__DEVICE_ID__", deviceId)
	}
	for i := range topicConfig.MultiSensor {
		topicConfig.MultiSensor[i].ResultTopic = strings.ReplaceAll(topicConfig.MultiSensor[i].ResultTopic, "__DEVICE_ID__", deviceId)
		topicConfig.MultiSensor[i].QueryTopic = strings.ReplaceAll(topicConfig.MultiSensor[i].QueryTopic, "__DEVICE_ID__", deviceId)
	}

	//populate entity names for metrics/logs: single sensors use their own
	//name, multi sensors (which have no config name) use their first value
	for i := range topicConfig.Sensor {
		topicConfig.Sensor[i].GeneralSensor.Name = topicConfig.Sensor[i].Name
	}
	for i := range topicConfig.MultiSensor {
		if len(topicConfig.MultiSensor[i].Values) > 0 {
			topicConfig.MultiSensor[i].GeneralSensor.Name = topicConfig.MultiSensor[i].Values[0].Name
			for _, value := range topicConfig.MultiSensor[i].Values[1:] {
				topicConfig.MultiSensor[i].Aliases = append(topicConfig.MultiSensor[i].Aliases, value.Name)
			}
		}
	}

	if err := topicConfig.validateRequestTopics(); err != nil {
		return TopicConfigurations{}, fmt.Errorf("invalid config: %w", err)
	}
	return topicConfig, nil
}

func (t *TopicConfigurations) Sensors() []GeneralSensor {
	sensors := make([]GeneralSensor, 0, len(t.Sensor)+len(t.MultiSensor))
	for _, sensor := range t.Sensor {
		sensors = append(sensors, sensor.GeneralSensor)
	}
	for _, sensor := range t.MultiSensor {
		sensors = append(sensors, sensor.GeneralSensor)
	}
	return sensors
}

func (t *TopicConfigurations) validate() error {
	if t.Availability != nil {
		if err := checkTopicName(t.Availability.Topic); err != nil {
			return fmt.Errorf("invalid availability topic: %w", err)
		}
	}

	sensorNames := map[string]bool{}
	for i, sensor := range t.Sensor {
		if err := validateSensor(sensor); err != nil {
			return fmt.Errorf("invalid sensor (#%d): %w", i, err)
		}

		if _, exists := sensorNames[sensor.Name]; exists {
			return fmt.Errorf("invalid sensor (#%d): sensor with this name already exists", i)
		}
		sensorNames[sensor.Name] = true
	}

	for i, sensor := range t.MultiSensor {
		if err := validateMultiSensor(sensor); err != nil {
			return fmt.Errorf("invalid multi sensor (#%d): %w", i, err)
		}

		for _, multiSensorValue := range sensor.Values {
			if _, exists := sensorNames[multiSensorValue.Name]; exists {
				return fmt.Errorf("invalid multi sensor (#%d): sensor with this name already exists", i)
			}
			sensorNames[multiSensorValue.Name] = true
		}
	}

	triggerNames := map[string]bool{}
	webhookIDs := map[string]bool{}
	for i, trigger := range t.Trigger {
		if err := validateTrigger(trigger); err != nil {
			return fmt.Errorf("invalid trigger (#%d): %w", i, err)
		}

		if _, exists := triggerNames[trigger.Name]; exists {
			return fmt.Errorf("invalid trigger (#%d): trigger with this name already exists", i)
		}
		triggerNames[trigger.Name] = true
		if trigger.WebhookID != "" {
			if webhookIDs[trigger.WebhookID] {
				return fmt.Errorf("duplicate webhook_id %q", trigger.WebhookID)
			}
			webhookIDs[trigger.WebhookID] = true
		}
	}

	return nil
}

func validateSensor(sensor Sensor) error {
	if sensor.Name == "" {
		return errors.New("name must not be empty")
	}
	return validateGeneralSensor(sensor.GeneralSensor)
}

func validateMultiSensor(sensor MultiSensor) error {
	if len(sensor.Values) == 0 {
		return errors.New("values must not be empty")
	}
	for _, multiSensorValue := range sensor.Values {
		if multiSensorValue.Name == "" {
			return errors.New("name must not be empty")
		}
		if multiSensorValue.Template == "" {
			return errors.New("template must not be empty")
		}
	}
	return validateGeneralSensor(sensor.GeneralSensor)
}

func validateGeneralSensor(sensor GeneralSensor) error {
	if sensor.Interval < 0 {
		return errors.New("interval must not be negative")
	}
	if sensor.QueryTopic != "" {
		if err := checkTopicName(sensor.QueryTopic); err != nil {
			return fmt.Errorf("invalid query_topic: %w", err)
		}
	}
	if err := validateTimeout(sensor.Timeout); err != nil {
		return err
	}
	if sensor.ResultTopic != "" || sensor.Interval > 0 || sensor.QueryTopic != "" {
		if err := checkTopicName(sensor.ResultTopic); err != nil {
			return fmt.Errorf("invalid topic: %w", err)
		}
	}
	if sensor.Command.Name == "" {
		return errors.New("command name must not be empty")
	}
	return nil
}

var webhookIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func validateTrigger(trigger Trigger) error {
	if trigger.Name == "" {
		return errors.New("name must not be empty")
	}
	if trigger.Topic != "" {
		if err := checkTopicName(trigger.Topic); err != nil {
			return fmt.Errorf("invalid topic: %w", err)
		}
	}
	if trigger.WebhookID != "" && !webhookIDPattern.MatchString(trigger.WebhookID) {
		return fmt.Errorf("invalid webhook_id %q: use lowercase letters, digits and hyphens", trigger.WebhookID)
	}
	if trigger.RunTopic != "" {
		if err := checkTopicName(trigger.RunTopic); err != nil {
			return fmt.Errorf("invalid run_topic: %w", err)
		}
	}
	if err := validateTimeout(trigger.Timeout); err != nil {
		return err
	}
	if trigger.Command.Name == "" {
		return errors.New("command name must not be empty")
	}
	return nil
}

// New request topics must be distinct from each other and from any existing
// ingress or publication topic. A collision could make a publish run commands.
func (t *TopicConfigurations) validateRequestTopics() error {
	used := make(map[string]bool)
	if t.Availability != nil {
		used[t.Availability.Topic] = true
	}
	for _, trigger := range t.Trigger {
		if trigger.Topic == "" {
			continue
		}
		used[trigger.Topic] = true
		used[trigger.Topic+"/STATE"] = true
		used[trigger.Topic+"/RESULT"] = true
	}
	sensors := t.Sensors()
	for _, sensor := range sensors {
		used[sensor.ResultTopic] = true
	}
	for _, trigger := range t.Trigger {
		if trigger.Topic == "" || trigger.RunTopic == "" {
			continue
		}
		if used[trigger.RunTopic] {
			return fmt.Errorf("run_topic %q is already used", trigger.RunTopic)
		}
		used[trigger.RunTopic] = true
	}
	for _, sensor := range sensors {
		if sensor.QueryTopic != "" && used[sensor.QueryTopic] {
			return fmt.Errorf("query_topic %q is already used", sensor.QueryTopic)
		}
		used[sensor.QueryTopic] = true
	}
	return nil
}

// validateTimeout rejects negative timeouts; zero means "no timeout".
func validateTimeout(timeout Interval) error {
	if timeout < 0 {
		return errors.New("timeout must not be negative")
	}
	return nil
}

var topicRegex = regexp.MustCompile(`^[a-zA-Z0-9_/]*$`)

func checkTopicName(topic string) error {
	if strings.Trim(topic, " ") == "" {
		return errors.New("must not be empty")
	}
	if !topicRegex.MatchString(topic) {
		return errors.New("invalid character")
	}

	return nil
}

// ValidateCommands checks that every configured command exists and is
// executable (fail fast at startup instead of failing at every run).
func (t *TopicConfigurations) ValidateCommands() error {
	commands := map[string]bool{}
	collect := func(command Command) {
		if command.Name != "" {
			commands[command.Name] = true
		}
	}
	for _, trigger := range t.Trigger {
		if trigger.Topic != "" || trigger.WebhookID != "" {
			collect(trigger.Command)
		}
	}
	for _, sensor := range t.Sensor {
		collect(sensor.Command)
	}
	for _, sensor := range t.MultiSensor {
		collect(sensor.Command)
	}

	//deterministic error order (map iteration order is random)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := checkCommand(name); err != nil {
			return fmt.Errorf("command %q: %w", name, err)
		}
	}
	return nil
}

func checkCommand(name string) error {
	if strings.ContainsRune(name, os.PathSeparator) {
		//absolute or relative path: must exist (and be executable on unix)
		info, err := os.Stat(name)
		if err != nil {
			return fmt.Errorf("not found: %w", err)
		}
		if info.IsDir() {
			return errors.New("is a directory")
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return errors.New("is not executable")
		}
		return nil
	}

	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("not found in PATH: %w", err)
	}
	return nil
}
