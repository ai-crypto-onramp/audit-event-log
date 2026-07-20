package event

import (
	"testing"
	"time"
)

func TestValidateNilEnvelope(t *testing.T) {
	var ev *Envelope
	if err := ev.Validate(); err == nil {
		t.Fatal("expected error for nil envelope")
	} else if !IsValidation(err) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidateMissingFields(t *testing.T) {
	cases := []struct {
		name string
		fix  func(*Envelope)
	}{
		{"missing ts", func(e *Envelope) { e.TS = time.Time{} }},
		{"missing actor_id", func(e *Envelope) { e.ActorID = "" }},
		{"missing action", func(e *Envelope) { e.Action = "" }},
		{"missing target_type", func(e *Envelope) { e.TargetType = "" }},
		{"missing target_id", func(e *Envelope) { e.TargetID = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := Envelope{
				ID:            "x",
				TS:            time.Now(),
				SourceService: "s",
				ActorID:       "a",
				Action:        "act",
				TargetType:    "tt",
				TargetID:      "ti",
				PayloadHash:   "sha256:abc",
			}
			c.fix(&ev)
			if err := ev.Validate(); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestDecodeMalformedJSON(t *testing.T) {
	if _, err := Decode([]byte("{not json")); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestIsHexFalse(t *testing.T) {
	if isHex("xyz") {
		t.Fatal("isHex should be false for non-hex chars")
	}
	if !isHex("0123456789abcdefABCDEF") {
		t.Fatal("isHex should be true for hex chars")
	}
}

func TestNormalizeHashBareNonHexLength64(t *testing.T) {
	// 64 chars but not all hex -> returned unchanged.
	bad := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	if got := NormalizeHash(bad); got != bad {
		t.Errorf("expected unchanged, got %q", got)
	}
}