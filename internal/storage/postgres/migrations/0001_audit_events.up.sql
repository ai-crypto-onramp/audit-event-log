CREATE TABLE IF NOT EXISTS audit_events (
    id             UUID        PRIMARY KEY,
    ts             TIMESTAMPTZ NOT NULL,
    source_service TEXT        NOT NULL,
    actor_id       TEXT        NOT NULL,
    action         TEXT        NOT NULL,
    target_type    TEXT        NOT NULL,
    target_id      TEXT        NOT NULL,
    payload_hash   BYTEA       NOT NULL,
    payload_ref    TEXT        NOT NULL,
    prev_hash     BYTEA       NOT NULL,
    this_hash     BYTEA       NOT NULL,
    anchored       BOOLEAN     NOT NULL DEFAULT FALSE,
    legal_hold     BOOLEAN     NOT NULL DEFAULT FALSE,
    redacted       BOOLEAN     NOT NULL DEFAULT FALSE
);

-- Composite index materializes the chain order (ts ASC, id ASC) used by the
-- hash-chain linkage and keyset pagination in the read API.
CREATE INDEX IF NOT EXISTS idx_audit_events_ts_id
    ON audit_events (ts ASC, id ASC);

-- Secondary indexes for the query paths documented in the README.
CREATE INDEX IF NOT EXISTS idx_audit_events_source_ts
    ON audit_events (source_service, ts);

CREATE INDEX IF NOT EXISTS idx_audit_events_actor_ts
    ON audit_events (actor_id, ts);

CREATE INDEX IF NOT EXISTS idx_audit_events_action_ts
    ON audit_events (action, ts);

CREATE INDEX IF NOT EXISTS idx_audit_events_target_ts
    ON audit_events (target_type, target_id, ts);