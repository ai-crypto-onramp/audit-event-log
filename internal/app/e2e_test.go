// Package app — end-to-end integration test covering the full pipeline
// (ingest -> chain -> anchor -> verify -> export) using in-memory fakes.
// This is the Stage 10 acceptance test: it exercises the wired app without
// any external dependency (Kafka, Postgres, S3, KMS are all fakes).
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-crypto-onramp/audit-event-log/internal/auth"
	"github.com/ai-crypto-onramp/audit-event-log/internal/chain"
	"github.com/ai-crypto-onramp/audit-event-log/internal/config"
	"github.com/ai-crypto-onramp/audit-event-log/internal/event"
	"github.com/ai-crypto-onramp/audit-event-log/internal/export"
	"github.com/ai-crypto-onramp/audit-event-log/internal/kafka"
	"github.com/ai-crypto-onramp/audit-event-log/internal/s3"
	"github.com/ai-crypto-onramp/audit-event-log/internal/store"
	"github.com/ai-crypto-onramp/audit-event-log/internal/store/memstore"
)

func ingestN(t *testing.T, srv *Server, n int) []*store.Event {
	t.Helper()
	ctx := context.Background()
	base, _ := time.Parse(time.RFC3339Nano, "2026-07-13T10:00:00Z")
	var events []*store.Event
	for i := 0; i < n; i++ {
		id := "e2e" + string(rune('a'+i))
		payload := map[string]any{"i": i}
		payloadBytes, _ := json.Marshal(payload)
		env := map[string]any{
			"id":             id,
			"ts":             base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"source_service": "orch",
			"actor_id":       "u1",
			"action":         "tx.initiated",
			"target_type":    "transaction",
			"target_id":      "tx" + string(rune('a'+i)),
			"payload":        payload,
			"payload_hash":   event.HashPayload(payloadBytes),
		}
		body, _ := json.Marshal(env)
		res := srv.Pipeline().Ingest(ctx, body)
		if !res.Inserted {
			t.Fatalf("ingest %s: %q", id, res.Reason)
		}
		e, _ := srv.Stores().Events.Get(ctx, id)
		events = append(events, e)
	}
	return events
}

func TestEndToEndIngestChainAnchorVerifyExport(t *testing.T) {
	cfg := WithDefaults(config.Config{
		PayloadBucket: "audit-bucket",
	})
	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	defer func() { _ = srv.Shutdown() }()
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()
	ctx := context.Background()

	// 1. Ingest 6 events.
	events := ingestN(t, srv, 6)
	if len(events) != 6 {
		t.Fatalf("ingested: %d", len(events))
	}

	// 2. Verify chain integrity via the admin endpoint.
	req, _ := http.NewRequest("POST", ts.URL+"/v1/admin/verify-chain?from=2026-07-13T00:00:00Z&to=2026-07-14T00:00:00Z", nil)
	req.Header.Set(auth.RolesHeader, string(auth.RoleAdmin))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify status: %d", resp.StatusCode)
	}
	var report chain.Report
	_ = json.NewDecoder(resp.Body).Decode(&report)
	resp.Body.Close()
	if report.Status != chain.StatusOK {
		t.Fatalf("verify status: %s", report.Status)
	}
	if report.EventCount != 6 {
		t.Errorf("event count: %d", report.EventCount)
	}

	// 3. Run the anchor job.
	if _, err := srv.Anchor().Run(ctx); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	head, _ := srv.Stores().Events.ChainHead(ctx)
	if !head.Anchored {
		t.Fatal("chain head not anchored")
	}
	anchors, _ := srv.Stores().Anchors.ListAnchors(ctx, time.Time{}, time.Time{})
	if len(anchors) != 1 {
		t.Fatalf("anchors: %d", len(anchors))
	}

	// 4. Tamper an event and re-verify -> should report broken.
	srv.Stores().Events.(*memstore.EventStore).TamperForTest(events[3].ID, func(e *store.Event) {
		e.ActorID = "tampered"
	})
	req, _ = http.NewRequest("POST", ts.URL+"/v1/admin/verify-chain?from=2026-07-13T00:00:00Z&to=2026-07-14T00:00:00Z", nil)
	req.Header.Set(auth.RolesHeader, string(auth.RoleAdmin))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&report)
	resp.Body.Close()
	if report.Status != chain.StatusBroken {
		t.Fatalf("expected broken, got %s (%s)", report.Status, report.Reason)
	}
	if report.FirstBroken != events[3].ID {
		t.Errorf("first broken: %s, want %s", report.FirstBroken, events[3].ID)
	}

	// 5. Create an export job (audit-admin).
	body, _ := json.Marshal(map[string]any{
		"query":         map[string]any{"service": "orch"},
		"format":        "csv",
		"retention_days": 100,
	})
	req, _ = http.NewRequest("POST", ts.URL+"/v1/exports", bytes.NewReader(body))
	req.Header.Set(auth.RolesHeader, string(auth.RoleAdmin))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create export: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create export status: %d", resp.StatusCode)
	}
	var createResp map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&createResp)
	resp.Body.Close()
	exportID := createResp["id"].(string)

	// 6. Run the export job via the runner (sync for test).
	fakeS3 := s3.NewFake()
	runner := export.New(export.Deps{
		Events:              srv.Stores().Events,
		Anchors:             srv.Stores().Anchors,
		Jobs:                srv.Stores().Exports,
		Payloads:            &s3PutReaderAdapter{fake: fakeS3},
		PayloadBucket:       "audit-bucket",
		DefaultRetentionDays: 2555,
	})
	job, _ := srv.Stores().Exports.GetJob(ctx, exportID)
	if err := runner.RunJob(ctx, job); err != nil {
		t.Fatalf("run job: %v", err)
	}
	job, _ = srv.Stores().Exports.GetJob(ctx, exportID)
	if job.Status != "complete" {
		t.Fatalf("job status: %s", job.Status)
	}
	if job.RowCount == 0 {
		t.Fatal("row count 0")
	}
	if job.PayloadRef == "" {
		t.Fatal("missing payload ref")
	}
}

// s3PutReaderAdapter adapts s3.Fake to export.PayloadStore.
type s3PutReaderAdapter struct {
	fake *s3.Fake
}

func (a *s3PutReaderAdapter) Put(ctx context.Context, bucket string, opts s3.PutOptions, body io.Reader) (string, error) {
	return a.fake.Put(ctx, bucket, opts, body)
}

// Use kafka.Fake type to keep the import for tests that construct one.
var _ = kafka.NewFake