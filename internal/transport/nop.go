package transport

// NopPublisher discards every publish. It is used in webhook-only mode (no
// broker configured): the ingress (HTTP) provides the results directly, so
// the MQTT side effects simply disappear.
type NopPublisher struct{}

func (NopPublisher) Publish(string, byte, bool, any) error { return nil }
