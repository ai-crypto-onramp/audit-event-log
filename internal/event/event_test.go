package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeAndValidate(t *testing.T) {
	raw := `{"id":"evt1","ts":"2026-07-13T10:00:00Z","source_service":"orch","actor_id":"u1","action":"tx.initiated","target_type":"transaction","target_id":"tx1","payload_hash":"sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","payload":{"amount":"100"}}`
	ev, err := DecodeAndValidate([]byte(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.ID != "evt1" {
		t.Errorf("id: %q", ev.ID)
	}
	if !ev.TS.Equal(time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("ts: %v", ev.TS)
	}
}

func TestValidateMissing(t *testing.T) {
	cases := []struct {
		name string
		ev   Envelope
	}{
		{"missing id", Envelope{TS: time.Now(), SourceService: "s", ActorID: "a", Action: "x", TargetType: "t", TargetID: "i", PayloadHash: "sha256:abc"}},
		{"missing source_service", Envelope{ID: "x", TS: time.Now(), ActorID: "a", Action: "x", TargetType: "t", TargetID: "i", PayloadHash: "sha256:abc"}},
		{"missing target_id", Envelope{ID: "x", TS: time.Now(), SourceService: "s", ActorID: "a", Action: "x", TargetType: "t", PayloadHash: "sha256:abc"}},
		{"missing payload_hash", Envelope{ID: "x", TS: time.Now(), SourceService: "s", ActorID: "a", Action: "x", TargetType: "t", TargetID: "i"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.ev.Validate()
			if !IsValidation(err) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestDecodeEmpty(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestHashPayloadAndVerify(t *testing.T) {
	payload := []byte(`{"a":1}`)
	h := HashPayload(payload)
	if h == "" || len(h) <= len("sha256:") {
		t.Fatalf("unexpected hash %q", h)
	}
	if !VerifyPayload(h, payload) {
		t.Fatal("VerifyPayload should accept correct hash")
	}
	if VerifyPayload(h, []byte("different")) {
		t.Fatal("VerifyPayload should reject wrong payload")
	}
	if !VerifyPayload(h[len("sha256:"):], payload) {
		t.Fatal("VerifyPayload should accept bare hex hash")
	}
}

func TestNormalizeHash(t *testing.T) {
	bare := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	prefixed := "sha256:" + bare
	if got := NormalizeHash(bare); got != prefixed {
		t.Errorf("NormalizeHash bare: %q", got)
	}
	if got := NormalizeHash(prefixed); got != prefixed {
		t.Errorf("NormalizeHash prefixed: %q", got)
	}
	if got := NormalizeHash(""); got != "" {
		t.Errorf("NormalizeHash empty: %q", got)
	}
	if got := NormalizeHash("notahash"); got != "notahash" {
		t.Errorf("NormalizeHash unknown: %q", got)
	}
}

func TestDecodeHashToBytes(t *testing.T) {
	bare := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	b, err := DecodeHashToBytes("sha256:" + bare)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("len: %d", len(b))
	}
	if _, err := DecodeHashToBytes("bad"); err == nil {
		t.Fatal("expected error for bad hash")
	}
	if EncodeHashFromBytes(b) != "sha256:"+bare {
		t.Error("EncodeHashFromBytes roundtrip")
	}
	if EncodeHashFromBytes([]byte{1, 2, 3}) != "" {
		t.Error("EncodeHashFromBytes wrong length should return empty")
	}
}

func TestEnvelopePayloadOptional(t *testing.T) {
	raw := `{"id":"e","ts":"2026-07-13T10:00:00Z","source_service":"s","actor_id":"a","action":"x","target_type":"t","target_id":"i","payload_hash":"sha256:abc"}`
	ev, err := DecodeAndValidate([]byte(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ev.Payload) != 0 {
		t.Errorf("payload should be absent, got %q", ev.Payload)
	}
}

func TestRoundtripJSON(t *testing.T) {
	ev := Envelope{
		ID:            "e1",
		TS:            time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		SourceService: "orch",
		ActorID:       "u1",
		Action:        "tx.initiated",
		TargetType:    "transaction",
		TargetID:      "tx1",
		PayloadHash:   "sha256:abc",
		Payload:       json.RawMessage(`{"k":"v"}`),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev2, err := Decode(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev2.ID != ev.ID || ev2.SourceService != ev.SourceService {
		t.Errorf("roundtrip mismatch: %+v", ev2)
	}
}