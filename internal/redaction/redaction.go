// Package redaction implements configurable PII redaction applied to event
// payloads before they are written to S3. The policy is a small YAML-like
// document declaring per-(source_service, action) field transforms:
// hash, mask, or drop. The original payload_hash is preserved over the
// un-redacted body so the hash chain integrity is unaffected.
//
// The package ships a tiny parser that accepts a subset of YAML sufficient
// for the redaction policy grammar; it avoids pulling in a YAML dependency.
// The accepted syntax is:
//
//   rules:
//     - service: orch
//       action: "*"
//       fields:
//         ssn: mask
//         email: hash
//         card_number: drop
//     - service: "*"
//       action: "*"
//       fields:
//         password: drop
//
// Field names are matched case-insensitively against the top-level keys of
// the payload JSON object.
package redaction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Transform names a redaction transform.
type Transform string

const (
	TransformNone  Transform = "none"
	TransformHash  Transform = "hash"
	TransformMask  Transform = "mask"
	TransformDrop  Transform = "drop"
)

// Rule declares which fields to transform for a (service, action) pair.
// Service and Action accept "*" as a wildcard.
type Rule struct {
	Service string
	Action  string
	Fields  map[string]Transform
}

// Policy is the loaded redaction policy.
type Policy struct {
	rules []Rule
}

// Apply returns a new redacted copy of the payload JSON body. It does not
// mutate the input. Fields not matched by any rule are passed through
// unchanged. The hash of the original body (computed by the caller) is
// preserved by the audit pipeline.
func (p *Policy) Apply(service, action string, body []byte) ([]byte, bool, error) {
	if p == nil || len(p.rules) == 0 || len(body) == 0 {
		return append([]byte(nil), body...), false, nil
	}
	matches := p.matches(service, action)
	if len(matches) == 0 {
		return append([]byte(nil), body...), false, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, false, fmt.Errorf("redaction: decode payload: %w", err)
	}
	changed := false
	for k := range obj {
		lower := strings.ToLower(k)
		for _, r := range matches {
			t, ok := r.Fields[lower]
			if !ok {
				continue
			}
			if t == TransformDrop {
				delete(obj, k)
				changed = true
				break
			}
			newVal, fieldChanged, err := applyTransform(t, obj[k])
			if err != nil {
				return nil, false, fmt.Errorf("redaction: transform %q on %q: %w", t, k, err)
			}
			if fieldChanged {
				obj[k] = newVal
				changed = true
			}
			break
		}
	}
	if !changed {
		return append([]byte(nil), body...), false, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, false, fmt.Errorf("redaction: encode payload: %w", err)
	}
	return out, true, nil
}

// matches returns the ordered list of rules that apply to the (service,
// action) pair. Wildcards ("*") match any value.
func (p *Policy) matches(service, action string) []Rule {
	var out []Rule
	for _, r := range p.rules {
		if r.Service != "*" && r.Service != service {
			continue
		}
		if r.Action != "*" && r.Action != action {
			continue
		}
		out = append(out, r)
	}
	return out
}

// applyTransform returns the transformed value. drop returns (nil, true,
// nil); the caller should remove the field. We signal drop with a sentinel
// via a bool flag rather than mutating the map here so the caller can decide.
func applyTransform(t Transform, v any) (any, bool, error) {
	switch t {
	case TransformNone:
		return v, false, nil
	case TransformHash:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false, err
		}
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:]), true, nil
	case TransformMask:
		s := asString(v)
		if s == "" {
			return v, false, nil
		}
		return maskString(s), true, nil
	case TransformDrop:
		return nil, true, nil
	default:
		return v, false, nil
	}
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// maskString masks all but the first and last character of a string. For
// strings shorter than 3 characters it returns a single "*".
func maskString(s string) string {
	if len(s) <= 2 {
		return "*"
	}
	return string(s[0]) + strings.Repeat("*", len(s)-2) + string(s[len(s)-1])
}

// IsDrop reports whether the transform value is drop, used by Apply to
// remove a field from the JSON object entirely.
func IsDrop(t Transform) bool { return t == TransformDrop }

// --- loader ---

// PolicyLoadError wraps a parsing error with the source path.
type PolicyLoadError struct {
	Path string
	Err  error
}

func (e *PolicyLoadError) Error() string { return "redaction: load " + e.Path + ": " + e.Err.Error() }

// LoadFile reads and parses a redaction policy file. Returns an empty
// (no-op) policy if path is empty or does not exist.
func LoadFile(path string) (*Policy, error) {
	if path == "" {
		return &Policy{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Policy{}, nil
		}
		return nil, &PolicyLoadError{Path: path, Err: err}
	}
	defer f.Close()
	return LoadReader(f)
}

// LoadReader parses a redaction policy from an io.Reader.
func LoadReader(r io.Reader) (*Policy, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Parse(string(data))
}

// Parse parses a redaction policy document. The grammar is a subset of YAML
// sufficient for the rule shape; see the package doc.
func Parse(text string) (*Policy, error) {
	p := &Policy{}
	lines := strings.Split(text, "\n")
	var current *Rule
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "rules:") {
			continue
		}
		if strings.HasPrefix(line, "- service:") {
			// start a new rule
			if current != nil {
				p.rules = append(p.rules, *current)
			}
			current = &Rule{Fields: map[string]Transform{}}
			current.Service = unquote(strings.TrimSpace(strings.TrimPrefix(line, "- service:")))
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "action:"):
			current.Action = unquote(strings.TrimSpace(strings.TrimPrefix(line, "action:")))
		case strings.HasPrefix(line, "fields:"):
			// no-op; fields follow indented
		case strings.Contains(line, ":"):
			// field: transform
			parts := strings.SplitN(line, ":", 2)
			field := strings.TrimSpace(parts[0])
			t := strings.TrimSpace(parts[1])
			if _, ok := current.Fields[field]; !ok {
				current.Fields[field] = Transform(t)
			}
		}
	}
	if current != nil {
		p.rules = append(p.rules, *current)
	}
	return p, nil
}

// unquote strips surrounding single or double quotes from s if present.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// Reloader wraps a Policy with a RWMutex so an admin endpoint can atomically
// swap the live policy without restart.
type Reloader struct {
	mu     sync.RWMutex
	policy *Policy
	path   string
}

// NewReloader returns a Reloader over the given path. The policy is loaded
// immediately; a missing file yields a no-op policy.
func NewReloader(path string) (*Reloader, error) {
	p, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return &Reloader{policy: p, path: path}, nil
}

// Policy returns the current policy.
func (r *Reloader) Policy() *Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policy
}

// Reload re-reads the policy file and atomically swaps it in.
func (r *Reloader) Reload() error {
	p, err := LoadFile(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.policy = p
	r.mu.Unlock()
	return nil
}

// Apply is a convenience that calls the current Policy's Apply.
func (r *Reloader) Apply(service, action string, body []byte) ([]byte, bool, error) {
	return r.Policy().Apply(service, action, body)
}