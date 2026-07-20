package s3adapter

import "testing"

func TestNewReturnsClient(t *testing.T) {
	c := New(nil)
	if c == nil {
		t.Fatal("nil client")
	}
}