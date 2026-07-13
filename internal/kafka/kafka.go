// Package kafka defines the consumer surface used by the audit service to
// drain events from the event bus. The in-memory Fake is used by tests and
// as the default when KAFKA_BROKERS is empty; the real kafka-go consumer
// lives in the kafkaadapter subpackage.
//
// Messages are delivered at-least-once; the consumer commits offsets only
// after the handler returns nil, mirroring commit-on-success semantics.
package kafka

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Message is a single consumed record.
type Message struct {
	Topic     string
	Partition int
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   map[string]string
}

// Handler processes a single message. Returning a non-nil error leaves the
// message uncommitted so it may be re-delivered; a nil error commits the
// offset.
type Handler func(ctx context.Context, msg Message) error

// ConsumerGroup is the consumer surface.
type ConsumerGroup interface {
	// Run blocks until ctx is canceled or the underlying reader returns a
	// fatal error. Each successfully handled message is committed.
	Run(ctx context.Context, handler Handler) error
	// Stop signals the consumer to drain and exit. It is safe to call after
	// Run has returned.
	Stop() error
}

// ErrInvalid is returned by constructors for invalid configuration.
type ErrInvalid struct{ Reason string }

func (e *ErrInvalid) Error() string { return "kafka: invalid: " + e.Reason }

// --- Fake consumer ---

// Fake is an in-memory ConsumerGroup for tests. It drains a caller-fed
// channel of Messages and invokes the handler for each. Stop closes the
// feed channel and waits for the in-flight handler to return.
type Fake struct {
	mu      sync.Mutex
	feed    chan Message
	done    chan struct{}
	stopped bool
	// committed records the offsets that were committed.
	committed []int64
}

// NewFake returns a Fake consumer with the given feed buffer size.
func NewFake(buffer int) *Fake {
	if buffer <= 0 {
		buffer = 64
	}
	return &Fake{feed: make(chan Message, buffer), done: make(chan struct{})}
}

// Enqueue pushes a message into the feed. It blocks if the buffer is full.
// Returns an error if the consumer has been stopped.
func (f *Fake) Enqueue(msg Message) error {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return errors.New("kafka: fake consumer stopped")
	}
	f.mu.Unlock()
	select {
	case f.feed <- msg:
		return nil
	default:
		return errors.New("kafka: fake feed full")
	}
}

// Run drains the feed and calls handler for each message.
func (f *Fake) Run(ctx context.Context, handler Handler) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-f.feed:
			if !ok {
				return nil
			}
			if err := handler(ctx, msg); err == nil {
				f.mu.Lock()
				f.committed = append(f.committed, msg.Offset)
				f.mu.Unlock()
			}
		}
	}
}

// Stop signals the consumer to drain. It does not interrupt an in-flight
// handler.
func (f *Fake) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil
	}
	f.stopped = true
	close(f.feed)
	return nil
}

// Committed returns the offsets that were successfully committed.
func (f *Fake) Committed() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int64, len(f.committed))
	copy(out, f.committed)
	return out
}

// Close is a no-op alias for Stop retained for backwards compatibility.
func (f *Fake) Close() error { return f.Stop() }

// --- Poll helper ---

// PollFeed returns the next message from a fake's internal feed without
// going through Run; used by tests that want to assert feed state directly.
// Returns false if no message is available within timeout.
func PollFeed(f *Fake, timeout time.Duration) (Message, bool) {
	select {
	case msg, ok := <-f.feed:
		return msg, ok
	case <-time.After(timeout):
		return Message{}, false
	}
}