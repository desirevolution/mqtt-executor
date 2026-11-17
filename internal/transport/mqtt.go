package transport

import (
	"errors"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// MQTTPublisher is a Publisher backed by a paho MQTT client. It waits for the
// paho token to complete and surfaces its error, so callers do not have to
// deal with tokens themselves.
type MQTTPublisher struct {
	client MQTT.Client
}

// NewMQTTPublisher wraps the given paho client into a Publisher.
func NewMQTTPublisher(client MQTT.Client) Publisher {
	return &MQTTPublisher{client: client}
}

func (p *MQTTPublisher) Publish(topic string, qos byte, retained bool, payload any) error {
	if !p.client.IsConnectionOpen() {
		return MQTT.ErrNotConnected
	}
	token := p.client.Publish(topic, qos, retained, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return errors.New("MQTT publish acknowledgement timed out")
	}
	return token.Error()
}
