// Package event defines the canonical audit envelope produced by upstream
// services over the Kafka event bus, the wire JSON format, validation, and
// SHA-256 payload hashing helpers used by ingest and the hash chain.
//
// The wire envelope is JSON with these required fields: id, ts,
// source_service, actor_id, action, target_type, target_id, payload_hash.
// prev_hash and this_hash are required on the wire per the README but the
// audit service recomputes this_hash from the indexed fields and sets
// prev_hash from the chain head at insert time, so any producer-supplied
// values are informational only.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Envelope is the canonical audit event.
type Envelope struct {
	ID            string          `json:"id"`
	TS            time.Time       `json:"ts"`
	SourceService string          `json:"source_service"`
	ActorID       string          `json:"actor_id"`
	Action        string          `json:"action"`
	TargetType    string          `json:"target_type"`
	TargetID      string          `json:"target_id"`
	PayloadHash   string          `json:"payload_hash"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	PrevHash      string          `json:"prev_hash,omitempty"`
	ThisHash      string          `json:"this_hash,omitempty"`
	Redaction     string          `json:"redaction,omitempty"`
}

// ErrValidation is returned by Validate when an envelope fails validation.
type ErrValidation struct{ Reason string }

func (e *ErrValidation) Error() string { return "event: invalid: " + e.Reason }

// IsValidation reports whether err is an ErrValidation.
func IsValidation(err error) bool {
	var v *ErrValidation
	return errors.As(err, &v)
}

// Validate enforces the required envelope fields. It returns a *ErrValidation
// describing the first missing field, or nil if the envelope is well-formed.
// It does NOT verify payload_hash against the payload body (use VerifyPayload
// for that) and does NOT recompute this_hash (ingest owns that).
func (e *Envelope) Validate() error {
	if e == nil {
		return &ErrValidation{Reason: "nil envelope"}
	}
	if e.ID == "" {
		return &ErrValidation{Reason: "missing id"}
	}
	if e.TS.IsZero() {
		return &ErrValidation{Reason: "missing ts"}
	}
	if e.SourceService == "" {
		return &ErrValidation{Reason: "missing source_service"}
	}
	if e.ActorID == "" {
		return &ErrValidation{Reason: "missing actor_id"}
	}
	if e.Action == "" {
		return &ErrValidation{Reason: "missing action"}
	}
	if e.TargetType == "" {
		return &ErrValidation{Reason: "missing target_type"}
	}
	if e.TargetID == "" {
		return &ErrValidation{Reason: "missing target_id"}
	}
	if e.PayloadHash == "" {
		return &ErrValidation{Reason: "missing payload_hash"}
	}
	return nil
}

// Decode unmarshals a JSON byte slice into an Envelope.
func Decode(b []byte) (*Envelope, error) {
	if len(b) == 0 {
		return nil, &ErrValidation{Reason: "empty payload"}
	}
	var ev Envelope
	if err := json.Unmarshal(b, &ev); err != nil {
		return nil, fmt.Errorf("event: decode: %w", err)
	}
	return &ev, nil
}

// DecodeAndValidate is a convenience wrapper that decodes and validates.
func DecodeAndValidate(b []byte) (*Envelope, error) {
	ev, err := Decode(b)
	if err != nil {
		return nil, err
	}
	if err := ev.Validate(); err != nil {
		return nil, err
	}
	return ev, nil
}

// HashPayload computes SHA-256 over the raw payload bytes and returns the
// canonical "sha256:<hex>" representation used by the wire envelope.
func HashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return HashPrefix + hex.EncodeToString(sum[:])
}

// VerifyPayload reports whether the supplied payload bytes hash to the
// given payload_hash. The hash may be "sha256:<hex>" or a bare hex string.
func VerifyPayload(payloadHash string, payload []byte) bool {
	want := NormalizeHash(payloadHash)
	return HashPayload(payload) == want
}

// HashPrefix is the canonical prefix for SHA-256 hashes in the wire format.
const HashPrefix = "sha256:"

// ZeroHash is the genesis prev_hash sentinel (all zero bytes).
const ZeroHash = HashPrefix + "0000000000000000000000000000000000000000000000000000000000000000"

// NormalizeHash returns the hash with a canonical "sha256:" prefix.
// It accepts bare hex strings and "sha256:<hex>" inputs; unknown shapes are
// returned unchanged.
func NormalizeHash(h string) string {
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, HashPrefix) {
		return h
	}
	// Bare hex of length 64 -> canonicalize.
	if len(h) == 64 && isHex(h) {
		return HashPrefix + h
	}
	return h
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// DecodeHashToBytes returns the 32 raw bytes of a "sha256:<hex>" or bare
// 64-char hex hash. Returns an error for malformed input.
func DecodeHashToBytes(h string) ([]byte, error) {
	s := strings.TrimPrefix(h, HashPrefix)
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("event: bad hash %q: %w", h, err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("event: hash %q is %d bytes, want 32", h, len(b))
	}
	return b, nil
}

// EncodeHashFromBytes returns the canonical "sha256:<hex>" for 32 bytes.
func EncodeHashFromBytes(b []byte) string {
	if len(b) != 32 {
		return ""
	}
	return HashPrefix + hex.EncodeToString(b)
}