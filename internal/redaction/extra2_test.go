package redaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyServiceMismatchContinue(t *testing.T) {
	// Rule with service "orch" should not match "pay"; the field is left
	// unchanged. This exercises the `continue` branch in matches() when
	// r.Service != "*" && r.Service != service.
	p, _ := Parse(`rules:
  - service: orch
    action: "*"
    fields:
      ssn: mask
`)
	out, changed, err := p.Apply("pay", "act", []byte(`{"ssn":"123-45-6789"}`))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("service mismatch should be no-op")
	}
	if string(out) != `{"ssn":"123-45-6789"}` {
		t.Errorf("out: %s", out)
	}
}

func TestApplyActionMismatchContinue(t *testing.T) {
	// Rule with action "create" should not match "update".
	p, _ := Parse(`rules:
  - service: "*"
    action: create
    fields:
      x: drop
`)
	out, changed, err := p.Apply("s", "update", []byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("action mismatch should be no-op")
	}
	if string(out) != `{"x":1}` {
		t.Errorf("out: %s", out)
	}
}

func TestReloadError(t *testing.T) {
	// Construct a Reloader over a real file, then replace the file with a
	// directory so the Reload open fails with a non-IsNotExist error,
	// exercising the Reload error-return path.
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte("rules:\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := NewReloader(path)
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	// Remove the file and create a directory at the same path so the
	// subsequent open returns "is a directory" (not IsNotExist).
	_ = os.Remove(path)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := r.Reload(); err == nil {
		t.Fatal("expected reload error")
	}
}

func TestNewReloaderLoadError(t *testing.T) {
	// Point NewReloader at a path inside a regular file so LoadFile
	// returns a *PolicyLoadError (non-IsNotExist), exercising the
	// NewReloader error-return branch.
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewReloader(regular + "/policy.yaml"); err == nil {
		t.Fatal("expected NewReloader error")
	}
}