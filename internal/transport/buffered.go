package transport

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

var errPublishQueueFull = errors.New("MQTT publish queue is full")
var errPublisherClosed = errors.New("MQTT publisher is closed")

type publication struct {
	topic    string
	qos      byte
	retained bool
	payload  any
	epoch    uint64
}

// BufferedPublisher delivers trigger events in FIFO order without waiting
// for the broker in the execution path. Pending events from a prior
// connection are discarded before the new connection's state is announced.
type BufferedPublisher struct {
	publisher Publisher
	connected func() bool
	mu        sync.Mutex
	queue     chan publication
	closed    bool
	epoch     uint64
	abort     chan struct{}
	done      chan struct{}
	abortOnce sync.Once
}

func NewBufferedPublisher(publisher Publisher, connected func() bool, capacity int) *BufferedPublisher {
	p := &BufferedPublisher{
		publisher: publisher,
		connected: connected,
		queue:     make(chan publication, capacity),
		abort:     make(chan struct{}),
		done:      make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *BufferedPublisher) Publish(topic string, qos byte, retained bool, payload any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return errPublisherClosed
	}
	if !p.connected() {
		return MQTT.ErrNotConnected
	}
	select {
	case p.queue <- publication{topic: topic, qos: qos, retained: retained, payload: payload, epoch: p.epoch}:
		return nil
	default:
		return errPublishQueueFull
	}
}

func (p *BufferedPublisher) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.epoch++
	for {
		select {
		case <-p.queue:
		default:
			return
		}
	}
}

func (p *BufferedPublisher) run() {
	defer close(p.done)
	for msg := range p.queue {
		select {
		case <-p.abort:
			return
		default:
		}
		p.mu.Lock()
		stale := msg.epoch != p.epoch
		p.mu.Unlock()
		if stale {
			continue
		}
		if err := p.publisher.Publish(msg.topic, msg.qos, msg.retained, msg.payload); err != nil {
			slog.Warn("Failed to deliver MQTT trigger update", "topic", msg.topic, "error", err)
		}
	}
}

func (p *BufferedPublisher) Close(timeout time.Duration) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-timer.C:
		p.abortOnce.Do(func() { close(p.abort) })
		return errors.New("timed out flushing MQTT trigger updates")
	}
}
