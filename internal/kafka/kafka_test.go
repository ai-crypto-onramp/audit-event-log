package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFakeRunAndCommit(t *testing.T) {
	f := NewFake(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed []int64
	var mu sync.Mutex
	handler := func(_ context.Context, msg Message) error {
		mu.Lock()
		processed = append(processed, msg.Offset)
		mu.Unlock()
		return nil
	}

	go func() { _ = f.Run(ctx, handler) }()

	for i := int64(1); i <= 3; i++ {
		if err := f.Enqueue(Message{Topic: "audit.v1", Offset: i, Value: []byte("x")}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	if err := waitFor(2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 3
	}); err != nil {
		t.Fatalf("processed: %v", err)
	}

	cancel()
	if got := f.Committed(); len(got) != 3 {
		t.Errorf("committed: %v", got)
	}
}

func TestFakeRedeliveryOnHandlerError(t *testing.T) {
	f := NewFake(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts int
	var mu sync.Mutex
	handler := func(_ context.Context, msg Message) error {
		mu.Lock()
		attempts++
		mu.Unlock()
		return errors.New("fail")
	}
	go func() { _ = f.Run(ctx, handler) }()

	_ = f.Enqueue(Message{Topic: "audit.v1", Offset: 1})
	_ = waitFor(500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return attempts >= 1
	})
	if got := f.Committed(); len(got) != 0 {
		t.Errorf("should not commit on handler error: %v", got)
	}
}

func TestFakeStop(t *testing.T) {
	f := NewFake(2)
	if err := f.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := f.Enqueue(Message{}); err == nil {
		t.Fatal("expected error after stop")
	}
	// Stop is idempotent.
	if err := f.Stop(); err != nil {
		t.Fatalf("stop again: %v", err)
	}
}

func TestPollFeed(t *testing.T) {
	f := NewFake(2)
	_ = f.Enqueue(Message{Topic: "audit.v1", Offset: 5})
	msg, ok := PollFeed(f, 100*time.Millisecond)
	if !ok {
		t.Fatal("poll timed out")
	}
	if msg.Offset != 5 {
		t.Errorf("offset: %d", msg.Offset)
	}
	_, ok = PollFeed(f, 50*time.Millisecond)
	if ok {
		t.Error("should be empty")
	}
}

func TestClose(t *testing.T) {
	f := NewFake(2)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close idempotent: %v", err)
	}
}

func TestErrInvalid(t *testing.T) {
	if (&ErrInvalid{Reason: "x"}).Error() != "kafka: invalid: x" {
		t.Error("error message")
	}
}

func TestEnqueueAfterStopReturnsError(t *testing.T) {
	f := NewFake(2)
	_ = f.Stop()
	if err := f.Enqueue(Message{}); err == nil {
		t.Fatal("expected error after stop")
	}
}

func TestEnqueueFullReturnsError(t *testing.T) {
	f := NewFake(1)
	_ = f.Enqueue(Message{Offset: 1})
	if err := f.Enqueue(Message{Offset: 2}); err == nil {
		t.Fatal("expected full error")
	}
}

// waitFor polls cond every 10ms until it returns true or timeout.
func waitFor(timeout time.Duration, cond func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cond() {
		return nil
	}
	return errors.New("waitFor: condition not met")
}