package config

import (
	"testing"
	"time"
)

func TestEnvBoolFalseValues(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off", "FALSE", "No", "OFF"} {
		t.Setenv("TEST_BOOL", v)
		if got := envBool("TEST_BOOL", true); got {
			t.Errorf("envBool(%q) should be false", v)
		}
	}
}

func TestEnvBoolUnknownReturnsDefault(t *testing.T) {
	t.Setenv("TEST_BOOL", "maybe")
	if got := envBool("TEST_BOOL", true); !got {
		t.Error("unknown value should return default")
	}
}

func TestEnvIntInvalid(t *testing.T) {
	t.Setenv("TEST_INT", "abc")
	if got := envInt("TEST_INT", 42); got != 42 {
		t.Errorf("invalid int should return default, got %d", got)
	}
}

func TestEnvDurInvalid(t *testing.T) {
	t.Setenv("TEST_DUR", "notaduration")
	if got := envDur("TEST_DUR", 5*time.Second); got != 5*time.Second {
		t.Errorf("invalid duration should return default, got %v", got)
	}
}