package transport

// Publisher abstracts the outbound channel of the executor. Today this is
// MQTT (see MQTTPublisher); the webhook ingress shares the same instance so
// that trigger/sensor side effects stay consistent. Implementations must be
// safe for concurrent use.
type Publisher interface {
	Publish(topic string, qos byte, retained bool, payload any) error
}
