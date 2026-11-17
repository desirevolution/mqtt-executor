package transport

import (
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
	token := p.client.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}
