# Project Plan — Audit / Event Log

Implementation plan for the append-only, tamper-evident, hash-chained audit trail
service. The service consumes domain events from the Kafka event bus, persists
them in an immutable WORM store with SHA-256 hash chaining, periodically anchors
the chain root to KMS, and serves compliance, regulator, and forensics queries.
Retention is 7+ years (2555 days) to satisfy SOX/AML record-keeping. Stages are
ordered so each builds on the previous deliverable toward a production-ready
compliance-grade audit log.

## Stage 1: DB Schema & S3 Payload Store

### Goal

Establish the searchable PostgreSQL index (`audit_events`) and the append-only
S3 payload bucket with Object Lock so subsequent stages have durable storage to
write into.

### Tasks

- [ ] Define `audit_events` table migration matching the README data model
      (id, ts, source_service, actor_id, action, target_type, target_id,
      payload_hash, payload_ref, prev_hash, this_hash, anchored, legal_hold,
      redacted).
- [ ] Add composite index on `(ts ASC, id ASC)` to materialize chain order.
- [ ] Add secondary indexes for query paths: `(source_service, ts)`,
      `(actor_id, ts)`, `(action, ts)`, `(target_type, target_id, ts)`.
- [ ] Provision S3 payload bucket with Object Lock (compliance mode) for the
      full retention period; expose `PAYLOAD_BUCKET` config.
- [ ] Configure S3 lifecycle: Standard -> Glacier at
      `GLACIER_TRANSITION_DAYS` (90), Glacier -> Deep Archive at
      `DEEP_ARCHIVE_TRANSITION_DAYS` (365).
- [ ] Add migration runner and idempotent schema bootstrap in `cmd/audit-event-log`.

### Acceptance criteria

- `go test ./internal/storage` creates the schema on a fresh Postgres and
  reports all expected columns and indexes.
- S3 bucket (or mocked equivalent) accepts a payload and rejects an overwrite
  during the Object Lock retention window.
- Lifecycle rules are codified and applied on bucket creation.

## Stage 2: Kafka Consumer & Structured Event Schema

### Goal

Stand up the Kafka consumer for the `audit.v1` topic and define the canonical
structured audit envelope (`actor`, `action`, `target`, `ts`, `source_service`,
`payload_hash`) used by every downstream stage.

### Tasks

- [ ] Implement Kafka consumer group `audit-event-log` on `KAFKA_TOPIC` with
      at-least-once delivery and commit-on-success offset handling.
- [ ] Define Go structs and JSON (un)marshaling for the wire envelope in
      `internal/event`.
- [ ] Validate required fields (`id`, `ts`, `source_service`, `actor_id`,
      `action`, `target_type`, `target_id`, `payload_hash`) and reject
      malformed events to a dead-letter path.
- [ ] Compute / verify `payload_hash` (SHA-256) when a `payload` is present;
      fetch out-of-band payload from S3 when only `payload_hash` is supplied.
- [ ] Wire config: `KAFKA_BROKERS`, `KAFKA_TOPIC`, `KAFKA_CONSUMER_GROUP`.

### Acceptance criteria

- Consumer processes a batch of well-formed events end-to-end (read -> validate
  -> log) without error.
- Malformed events are routed to the dead-letter path and do not block the
  consumer.
- `payload_hash` mismatch between supplied payload and hash causes the event to
  be rejected.

## Stage 3: Append-Only Ingest with Dedup

### Goal

Persist validated events into PostgreSQL and S3 as an append-only, idempotent
write path keyed on event `id`.

### Tasks

- [ ] Insert into `audit_events` and write payload to S3 (`payload_ref`) in a
      single coordinated flow; S3 write precedes the index commit.
- [ ] Enforce idempotency: insert with `ON CONFLICT (id) DO NOTHING` and treat
      re-deliveries as no-ops.
- [ ] Block updates/deletes at the repository layer; add tests asserting no
      mutating methods exist.
- [ ] Surface ingest metrics: events/sec, latency p99, duplicate count,
      rejection count.
- [ ] Meet ingest latency target (< 1s p99 event -> persisted + indexed).

### Acceptance criteria

- Re-delivering the same event `id` produces exactly one row and one S3 object.
- No code path exposes update or delete of an existing `audit_events` row.
- p99 ingest latency under load test is < 1s.

## Stage 4: Hash Chain & Periodic KMS Anchoring

### Goal

Materialize the tamper-evident SHA-256 hash chain (`prev_hash` / `this_hash`)
and periodically anchor the chain root to KMS (and optional external notary).

### Tasks

- [ ] Compute `this_hash` = SHA-256 over canonical concatenation of (id, ts,
      source_service, actor_id, action, target_type, target_id, payload_hash,
      prev_hash).
- [ ] On ingest, set `prev_hash` = `this_hash` of the preceding event ordered
      by `(ts ASC, id ASC)`; genesis event `prev_hash` = 0.
- [ ] Handle concurrent inserts safely (chain head lookup under transactional
      lock / serializable isolation).
- [ ] Implement anchor job: every `CHAIN_ANCHOR_INTERVAL_MINUTES` (default 60),
      write the latest `this_hash` to a root anchor record signed by
      `KMS_KEY_ID`; store anchor in S3 and POST to `EXTERNAL_NOTARY_URL` if set.
- [ ] Set `anchored = true` on covered events after a successful anchor write.

### Acceptance criteria

- Recomputing `this_hash` for every event in a test chain matches stored values
  and `prev_hash` linkage is contiguous.
- Anchor job emits a KMS-signed anchor record and flips `anchored` on covered
  events.
- A tampered row is detectable by recomputation in the test harness.

## Stage 5: REST Read API (Filtered Search)

### Goal

Expose the read path over HTTPS so incident response and compliance teams can
search events by service, actor, action, target, and time range.

### Tasks

- [ ] Implement `GET /v1/events` with filters `from`, `to`, `service`, `actor`,
      `action`, `target_type`, `target_id`, `limit`, `cursor` (keyset
      pagination on `(ts, id)`).
- [ ] Implement `GET /v1/events/:id` returning the full envelope plus a
      presigned S3 `payload_ref` download URL.
- [ ] Implement `GET /v1/events/:id/verify-chain` returning
      `{ prev_hash, this_hash, status }` for a single event.
- [ ] Add authn/authz middleware (audit-reader role for reads; audit-admin for
      admin endpoints introduced later).
- [ ] Hit query p99 < 500ms on indexed lookups under load.

### Acceptance criteria

- All filter combinations return correct, paginated results.
- A single-event fetch returns envelope + usable presigned payload URL.
- `/verify-chain` for a tampered event returns status `broken`.
- p99 read latency < 500ms in the load test.

## Stage 6: Chain Integrity Verification Tool

### Goal

Provide a CLI / admin endpoint that walks the entire chain, recomputes hashes,
and reports the first broken link and any gap.

### Tasks

- [ ] Implement `./bin/audit-event-log verify-chain --from --to` CLI that
      streams events ordered by `(ts, id)` and recomputes `this_hash` and
      `prev_hash` linkage.
- [ ] Implement `POST /v1/admin/verify-chain` (audit-admin only) triggering a
      full sweep; return progress + final signed report.
- [ ] Validate each KMS-signed anchor against the recomputed root at that
      point; report first mismatch with position and anchor id.
- [ ] Produce a signed integrity report artifact suitable for regulator
      submission.
- [ ] Target full-chain recompute <= 30 minutes for a full sweep.

### Acceptance criteria

- CLI detects a single-bit modification and reports the offending event id and
  position.
- A gap (missing `prev_hash` linkage) is reported with the boundary ids.
- KMS anchor mismatch is reported with the anchor id and expected vs actual
  root.

## Stage 7: Regulator Export Endpoint (JSON/CSV)

### Goal

Produce signed, immutable regulator-grade exports in JSON or CSV over a
configurable query window and retention period.

### Tasks

- [ ] Implement `POST /v1/exports` accepting
      `{ query, format: json|csv, retention_days }`; create an async export job
      and return export id.
- [ ] Implement `GET /v1/exports/:id` returning job status and, on completion,
      a signed S3 download URL.
- [ ] Write export artifact to S3 with Object Lock for `retention_days`; same
      envelope schema as stored events plus `prev_hash` / `this_hash`.
- [ ] Include a manifest (row count, window, chain root, KMS anchor id).
- [ ] Restrict endpoint to audit-admin role.

### Acceptance criteria

- A JSON and a CSV export for the same query yield row-count-consistent,
      schema-consistent artifacts.
- Export artifact is immutable for `retention_days` (Object Lock enforced).
- Download URL is signed and time-limited.

## Stage 8: PII Redaction Policy

### Goal

Prevent the audit store from becoming a plaintext PII repository by applying
configurable redaction at ingest without breaking the hash chain.

### Tasks

- [ ] Define redaction policy format (`REDACTION_POLICY_PATH`,
      `/etc/audit/redaction.yaml`) declaring field-level transforms (hash,
      mask, drop) per `source_service` / `action`.
- [ ] Apply redaction to `payload` before writing to S3; preserve original
      `payload_hash` and set `redacted = true`.
- [ ] Ensure `this_hash` is computed over `payload_hash` (not the payload body)
      so redaction does not invalidate the chain.
- [ ] Add admin endpoint to reload redaction policy without restart.
- [ ] Add tests covering hash, mask, and drop transforms and policy reload.

### Acceptance criteria

- A PII field configured for `mask` is stored masked in S3; `payload_hash`
  still matches the original (pre-redaction) payload.
- Chain verification passes on a redacted event.
- Policy reload takes effect for events ingested after the reload.

## Stage 9: Retention 2555 Days + WORM + Glacier Cold Tier

### Goal

Enforce the 7+ year (2555-day default) retention with WORM semantics and cold
tier lifecycle to Glacier / Deep Archive.

### Tasks

- [ ] Enforce `RETENTION_DAYS` (default 2555) as Object Lock retention on all
      S3 payload and export artifacts.
- [ ] Confirm lifecycle transitions Standard -> Glacier (90d) ->
      Deep Archive (365d) are active and tested against a mocked S3.
- [ ] Implement legal hold: `POST /v1/admin/legal-hold/:id` to place/release
      hold; held events are exempt from any lifecycle delete and remain
      queryable/exportable.
- [ ] Default new events to `LEGAL_HOLD_DEFAULT` on ingest.
- [ ] Document jurisdiction override via `RETENTION_DAYS`.

### Acceptance criteria

- An object under retention cannot be overwritten or deleted via the S3 API.
- Legal-hold events survive a lifecycle sweep and remain queryable.
- `RETENTION_DAYS` is configurable and applied at object write time.

## Stage 10: Tests, Coverage & Docker

### Goal

Reach production-grade confidence: comprehensive unit + integration tests,
coverage gating, and a reproducible container build.

### Tasks

- [ ] Unit tests for chain hashing, dedup, redaction, pagination, and
  export serialization.
- [ ] Integration tests with Kafka, Postgres, S3-compatible store, and KMS
  mock covering ingest -> chain -> anchor -> verify -> export.
- [ ] Tamper-detection regression test: flip a bit, assert verifier reports
  the offending event.
- [ ] Coverage gate in CI (codecov) with target >= 80% on `internal/...`.
- [ ] Docker image build (`Dockerfile`) and `Makefile` targets for
  build/test/lint/coverage.
- [ ] README "Local Development" section verified end-to-end.

### Acceptance criteria

- `go test ./...` passes with >= 80% coverage on internal packages.
- `make docker` produces a runnable image that passes the smoke test.
- Tamper-detection test fails if the verifier ever misses a modification.