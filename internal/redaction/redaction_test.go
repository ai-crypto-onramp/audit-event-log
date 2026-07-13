package redaction

import (
	"encoding/json"
	"strings"
	"testing"
)

const samplePolicy = `rules:
  - service: orch
    action: "*"
    fields:
      ssn: mask
      email: hash
      card_number: drop
  - service: "*"
    action: "*"
    fields:
      password: drop
      secret: mask
`

func TestParseSample(t *testing.T) {
	p, err := Parse(samplePolicy)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.rules) != 2 {
		t.Fatalf("rules: %d", len(p.rules))
	}
	if p.rules[0].Service != "orch" || p.rules[0].Action != "*" {
		t.Errorf("rule0: %+v", p.rules[0])
	}
	if p.rules[0].Fields["ssn"] != TransformMask {
		t.Errorf("ssn: %q", p.rules[0].Fields["ssn"])
	}
	if p.rules[0].Fields["email"] != TransformHash {
		t.Errorf("email: %q", p.rules[0].Fields["email"])
	}
	if p.rules[0].Fields["card_number"] != TransformDrop {
		t.Errorf("card_number: %q", p.rules[0].Fields["card_number"])
	}
}

func TestApplyMask(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"ssn":"123-45-6789","amount":"100"}`)
	out, changed, err := p.Apply("orch", "tx.initiated", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	if obj["ssn"] != "1*********9" {
		t.Errorf("ssn masked: %q", obj["ssn"])
	}
	if obj["amount"] != "100" {
		t.Errorf("amount changed: %q", obj["amount"])
	}
}

func TestApplyHash(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"email":"user@example.com"}`)
	out, changed, err := p.Apply("orch", "act", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	h, ok := obj["email"].(string)
	if !ok || !strings.HasPrefix(h, "sha256:") {
		t.Errorf("email hashed: %q", obj["email"])
	}
}

func TestApplyDrop(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"card_number":"4111111111111111","amount":"100"}`)
	out, changed, err := p.Apply("orch", "act", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	if _, ok := obj["card_number"]; ok {
		t.Error("card_number should be dropped")
	}
	if obj["amount"] != "100" {
		t.Errorf("amount changed: %q", obj["amount"])
	}
}

func TestApplyWildcardService(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"password":"secret","amount":"100"}`)
	out, changed, err := p.Apply("pay", "act", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	if _, ok := obj["password"]; ok {
		t.Error("password should be dropped")
	}
}

func TestApplyNoMatch(t *testing.T) {
	p, _ := Parse(samplePolicy)
	body := []byte(`{"amount":"100"}`)
	out, changed, err := p.Apply("orch", "act", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("should be no-op")
	}
	if string(out) != string(body) {
		t.Error("body should be unchanged")
	}
}

func TestApplyNilPolicy(t *testing.T) {
	var p *Policy
	body := []byte(`{"a":1}`)
	out, changed, err := p.Apply("orch", "act", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("nil policy should be no-op")
	}
	if string(out) != string(body) {
		t.Error("body should be unchanged")
	}
}

func TestMaskStringShort(t *testing.T) {
	if maskString("") != "*" {
		t.Errorf("empty: %q", maskString(""))
	}
	if maskString("a") != "*" {
		t.Errorf("a: %q", maskString("a"))
	}
	if maskString("ab") != "*" {
		t.Errorf("ab: %q", maskString("ab"))
	}
	if maskString("abc") != "a*c" {
		t.Errorf("abc: %q", maskString("abc"))
	}
}

func TestLoadFileMissing(t *testing.T) {
	p, err := LoadFile("/no/such/path/redaction.yaml")
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if p == nil || len(p.rules) != 0 {
		t.Error("expected empty policy for missing file")
	}
}

func TestLoadFileEmpty(t *testing.T) {
	p, err := LoadFile("")
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if p == nil {
		t.Fatal("nil policy")
	}
}

func TestReloaderReload(t *testing.T) {
	// Use a temp file via the test; we'd need os.WriteFile. Keep it simple:
	// create a Reloader over a missing path (no-op policy), then swap by
	// reusing Parse internally via Reload (which re-reads the same missing
	// path and stays no-op).
	r, err := NewReloader("/no/such/path/redaction.yaml")
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	if r.Policy() == nil {
		t.Fatal("nil policy")
	}
	body := []byte(`{"a":1}`)
	out, changed, err := r.Apply("s", "a", body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("should be no-op")
	}
	if string(out) != string(body) {
		t.Error("body changed")
	}
	if err := r.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
}