-- audit_events: append-only, hash-chained, searchable index.
-- One row per ingested audit event. Immutable after insert; no UPDATE or
-- DELETE path is exposed at the application layer.
CREATE TABLE IF NOT EXISTS audit_events (
    id              UUID        PRIMARY KEY,
    ts              TIMESTAMPTZ NOT NULL,
    source_service  TEXT        NOT NULL,
    actor_id        TEXT        NOT NULL,
    action          TEXT        NOT NULL,
    target_type     TEXT        NOT NULL,
    target_id       TEXT        NOT NULL,
    payload_hash    BYTEA       NOT NULL,
    payload_ref     TEXT        NOT NULL,
    prev_hash       BYTEA       NOT NULL,
    this_hash       BYTEA       NOT NULL,
    anchored        BOOLEAN     NOT NULL DEFAULT false,
    legal_hold     BOOLEAN     NOT NULL DEFAULT false,
    redacted        BOOLEAN     NOT NULL DEFAULT false
);

-- Composite index materializing chain order.
CREATE INDEX IF NOT EXISTS audit_events_ts_id_idx ON audit_events (ts ASC, id ASC);

-- Secondary indexes for query paths.
CREATE INDEX IF NOT EXISTS audit_events_service_ts_idx ON audit_events (source_service, ts);
CREATE INDEX IF NOT EXISTS audit_events_actor_ts_idx   ON audit_events (actor_id, ts);
CREATE INDEX IF NOT EXISTS audit_events_action_ts_idx   ON audit_events (action, ts);
CREATE INDEX IF NOT EXISTS audit_events_target_ts_idx   ON audit_events (target_type, target_id, ts);

-- Anchor records: one row per periodic KMS-signed Merkle root anchor.
CREATE TABLE IF NOT EXISTS chain_anchors (
    id              BIGSERIAL    PRIMARY KEY,
    anchored_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    root_hash       BYTEA       NOT NULL,
    last_event_id   UUID,
    last_event_ts   TIMESTAMPTZ,
    signature       BYTEA       NOT NULL,
    kms_key_id      TEXT         NOT NULL DEFAULT '',
    notary_url      TEXT         NOT NULL DEFAULT '',
    event_count     BIGINT       NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS chain_anchors_anchored_at_idx ON chain_anchors (anchored_at);

-- Export jobs: regulator export state.
CREATE TABLE IF NOT EXISTS export_jobs (
    id              UUID        PRIMARY KEY,
    query           JSONB       NOT NULL DEFAULT '{}',
    format          TEXT        NOT NULL DEFAULT 'json',
    retention_days  INT         NOT NULL DEFAULT 2555,
    status          TEXT        NOT NULL DEFAULT 'pending',
    row_count       BIGINT      NOT NULL DEFAULT 0,
    payload_ref     TEXT        NOT NULL DEFAULT '',
    chain_root      BYTEA,
    anchor_id       BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS export_jobs_status_idx ON export_jobs (status);

-- Dead-letter: events rejected at ingest for invalid payload/schema.
CREATE TABLE IF NOT EXISTS dead_letter (
    id              BIGSERIAL    PRIMARY KEY,
    topic           TEXT         NOT NULL DEFAULT '',
    partition_no    INT          NOT NULL DEFAULT 0,
    offset_no       BIGINT       NOT NULL DEFAULT 0,
    key             TEXT         NOT NULL DEFAULT '',
    payload         BYTEA        NOT NULL,
    reason          TEXT         NOT NULL,
    rejected_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dead_letter_rejected_at_idx ON dead_letter (rejected_at);