package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegisterNilRegisterer(t *testing.T) {
	// Register with nil should use DefaultRegisterer. Safe to call multiple
	// times (sync.Once); should not panic.
	Register(nil)
	Register(prometheus.DefaultRegisterer)
}