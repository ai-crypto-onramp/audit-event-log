package kafka

import (
	"context"
	"testing"
	"time"
)

func TestNewFakeBufferDefault(t *testing.T) {
	// buffer <= 0 should clamp to 64.
	f := NewFake(0)
	if cap(f.feed) != 64 {
		t.Errorf("expected default buffer 64, got %d", cap(f.feed))
	}
	f2 := NewFake(-5)
	if cap(f2.feed) != 64 {
		t.Errorf("expected default buffer 64 for negative, got %d", cap(f2.feed))
	}
}

func TestFakeRunReturnsOnClosedFeed(t *testing.T) {
	f := NewFake(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Close the feed before running; Run should return nil.
	_ = f.Stop()
	errCh := make(chan error, 1)
	go func() { errCh <- f.Run(ctx, func(context.Context, Message) error { return nil }) }()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected nil on closed feed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return on closed feed")
	}
}