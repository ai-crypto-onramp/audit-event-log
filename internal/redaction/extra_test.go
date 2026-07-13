package redaction

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyTransformUnknown(t *testing.T) {
	v, changed, err := applyTransform(Transform("bogus"), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("should not change")
	}
	if v != "x" {
		t.Fatalf("v: %v", v)
	}
}

func TestApplyTransformNone(t *testing.T) {
	v, changed, err := applyTransform(TransformNone, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("should not change")
	}
	if v != "x" {
		t.Fatalf("v: %v", v)
	}
}

func TestApplyTransformHashError(t *testing.T) {
	_, _, err := applyTransform(TransformHash, make(chan int))
	if err == nil {
		t.Fatal("expected hash error for unmarshalable value")
	}
}

func TestApplyTransformMaskNonString(t *testing.T) {
	v, changed, err := applyTransform(TransformMask, 42)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if v != "*" {
		t.Fatalf("masked number: %v", v)
	}
}

func TestApplyTransformMaskEmpty(t *testing.T) {
	v, changed, err := applyTransform(TransformMask, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("empty string should not change")
	}
	if v != "" {
		t.Fatalf("v: %v", v)
	}
}

func TestApplyTransformDropValue(t *testing.T) {
	v, changed, err := applyTransform(TransformDrop, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatal("drop should change")
	}
	if v != nil {
		t.Fatalf("v: %v", v)
	}
}

func TestAsStringStringer(t *testing.T) {
	got := asString(myString("hello"))
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

type myString string

func (m myString) String() string { return string(m) }

func TestAsStringNumber(t *testing.T) {
	got := asString(42)
	if got != "42" {
		t.Fatalf("got %q", got)
	}
}

func TestIsDrop(t *testing.T) {
	if !IsDrop(TransformDrop) {
		t.Fatal("drop should be drop")
	}
	if IsDrop(TransformMask) {
		t.Fatal("mask is not drop")
	}
}

func TestReloaderApplyAndPolicySwap(t *testing.T) {
	r, err := NewReloader("")
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	out, changed, err := r.Apply("s", "a", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("should be no-op")
	}
	if string(out) != `{"a":1}` {
		t.Fatalf("out: %s", out)
	}
	if r.Policy() == nil {
		t.Fatal("nil policy")
	}
}

func TestLoadReader(t *testing.T) {
	p, err := LoadReader(strings.NewReader(samplePolicyYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(p.rules) != 1 {
		t.Fatalf("rules: %d", len(p.rules))
	}
}

func TestLoadReaderIOError(t *testing.T) {
	_, err := LoadReader(errReader{})
	if err == nil {
		t.Fatal("expected io error")
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("io boom") }

const samplePolicyYAML = `rules:
  - service: "*"
    action: "*"
    fields:
      password: drop
`

func TestApplyDecodeError(t *testing.T) {
	p, _ := Parse(samplePolicy)
	if _, _, err := p.Apply("orch", "act", []byte("not-json")); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestApplyEmptyBody(t *testing.T) {
	p, _ := Parse(samplePolicy)
	out, changed, err := p.Apply("orch", "act", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("empty body should be no-op")
	}
	if len(out) != 0 {
		t.Fatalf("out: %v", out)
	}
}

func TestApplyNoMatchStillPassthrough(t *testing.T) {
	p, _ := Parse(samplePolicy)
	out, changed, err := p.Apply("unknown-svc", "act", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("no match should be no-op")
	}
	if string(out) != `{"a":1}` {
		t.Fatalf("out: %s", out)
	}
}

func TestApplyUnchangedPath(t *testing.T) {
	// Rule matches but field doesn't change (transform none on matching field).
	pol := `rules:
  - service: "*"
    action: "*"
    fields:
      amount: none
`
	p, _ := Parse(pol)
	out, changed, err := p.Apply("s", "a", []byte(`{"amount":100}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("none transform should not flag changed")
	}
	if string(out) != `{"amount":100}` {
		t.Fatalf("out: %s", out)
	}
}

func TestMatchesServiceAndActionFiltering(t *testing.T) {
	pol := `rules:
  - service: orch
    action: create
    fields:
      x: drop
  - service: orch
    action: "*"
    fields:
      y: drop
  - service: "*"
    action: create
    fields:
      z: drop
`
	p, _ := Parse(pol)
	m := p.matches("orch", "create")
	if len(m) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m))
	}
	m = p.matches("orch", "update")
	if len(m) != 1 {
		t.Fatalf("expected 1 match for orch/update, got %d", len(m))
	}
	m = p.matches("pay", "create")
	if len(m) != 1 {
		t.Fatalf("expected 1 match for pay/create, got %d", len(m))
	}
	m = p.matches("pay", "update")
	if len(m) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(m))
	}
}

func TestLoadFileActualFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(samplePolicy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(p.rules) != 2 {
		t.Fatalf("rules: %d", len(p.rules))
	}
}

func TestLoadFileOpenError(t *testing.T) {
	dir := t.TempDir()
	// Open a path inside a file (not a directory) to force a not-exist error
	// that isn't caught by os.IsNotExist.
	filePath := filepath.Join(dir, "regular")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadFile(filePath + "/policy.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReloaderReloadFromRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(samplePolicy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := NewReloader(path)
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	if len(r.Policy().rules) != 2 {
		t.Fatalf("rules: %d", len(r.Policy().rules))
	}
	// Rewrite with a single rule and reload.
	if err := os.WriteFile(path, []byte(samplePolicyYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(r.Policy().rules) != 1 {
		t.Fatalf("after reload rules: %d", len(r.Policy().rules))
	}
}

func TestReloaderApplyRealPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(samplePolicy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := NewReloader(path)
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	out, changed, err := r.Apply("orch", "act", []byte(`{"password":"x","amount":"1"}`))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if strings.Contains(string(out), "password") {
		t.Fatalf("password not dropped: %s", out)
	}
}

func TestParseCommentsAndBlankLines(t *testing.T) {
	pol := `# top comment

rules:
  # rule comment
  - service: orch
    action: "*"
    fields:
      ssn: mask

  - service: "*"
    action: "*"
    fields:
      password: drop
`
	p, err := Parse(pol)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.rules) != 2 {
		t.Fatalf("rules: %d", len(p.rules))
	}
}

func TestParseQuotedValues(t *testing.T) {
	pol := `rules:
  - service: "orch"
    action: '*'
    fields:
      ssn: mask
`
	p, err := Parse(pol)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.rules[0].Service != "orch" {
		t.Errorf("service: %q", p.rules[0].Service)
	}
	if p.rules[0].Action != "*" {
		t.Errorf("action: %q", p.rules[0].Action)
	}
	if p.rules[0].Fields["ssn"] != TransformMask {
		t.Errorf("ssn: %q", p.rules[0].Fields["ssn"])
	}
}

func TestParseIgnoreStrayLines(t *testing.T) {
	pol := `stray line without colon
rules:
  - service: orch
    action: "*"
    fields:
      ssn: mask
`
	p, err := Parse(pol)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.rules) != 1 {
		t.Fatalf("rules: %d", len(p.rules))
	}
}

func TestPolicyLoadErrorMethod(t *testing.T) {
	e := &PolicyLoadError{Path: "/x", Err: errors.New("boom")}
	if !strings.Contains(e.Error(), "/x") || !strings.Contains(e.Error(), "boom") {
		t.Fatalf("err: %s", e.Error())
	}
}

func TestApplyFieldCaseInsensitive(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"SSN":"123-45-6789"}`)
	out, changed, err := p.Apply("orch", "act", body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatal("expected changed (case-insensitive match)")
	}
	if !strings.Contains(string(out), "1*********9") {
		t.Fatalf("SSN not masked: %s", out)
	}
}
