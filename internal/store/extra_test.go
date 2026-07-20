package store

import (
	"errors"
	"testing"
	"time"
)

func TestCursorStringZero(t *testing.T) {
	c := Cursor{}
	if got := c.String(); got != "" {
		t.Errorf("zero cursor String: %q", got)
	}
}

func TestCursorStringNonZero(t *testing.T) {
	ts := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	c := Cursor{TS: ts, ID: "e1"}
	if got := c.String(); got != "2026-07-13T10:00:00Z|e1" {
		t.Errorf("cursor String: %q", got)
	}
}

func TestParseCursorLoop(t *testing.T) {
	// Valid cursor with a pipe separator and a timestamp.
	c, err := ParseCursor("2026-07-13T10:00:00Z|e1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.ID != "e1" || !c.TS.Equal(time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("parsed: %+v", c)
	}
}

func TestParseCursorBadTimestamp(t *testing.T) {
	if _, err := ParseCursor("notatime|e1"); err == nil {
		t.Fatal("expected error for bad timestamp")
	}
}

func TestParseCursorNoPipe(t *testing.T) {
	if _, err := ParseCursor("nodelimiter"); err == nil {
		t.Fatal("expected error for cursor without delimiter")
	}
}

func TestErrNotFoundError(t *testing.T) {
	e := &ErrNotFound{ID: "abc"}
	if got := e.Error(); got != "store: not found: abc" {
		t.Errorf("error: %q", got)
	}
}

func TestIsNotFoundFalse(t *testing.T) {
	if IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound should be false for non-ErrNotFound")
	}
	if IsNotFound(nil) {
		t.Fatal("IsNotFound should be false for nil")
	}
}

func TestIsNotFoundTrue(t *testing.T) {
	if !IsNotFound(&ErrNotFound{ID: "x"}) {
		t.Fatal("IsNotFound should be true for ErrNotFound")
	}
}