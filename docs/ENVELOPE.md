# Audit Event Envelope v1

This is the canonical envelope every producer publishes to Kafka topic
`audit.v1`. The audit-event-log consumer
(`audit-event-log/internal/event/event.go::Envelope`) decodes and validates
this exact shape; the canonical schema is also published in
`contracts/proto/audit/v1/events.proto` and `contracts/asyncapi/audit/v1/asyncapi.yaml` (now under `.github/contracts/`).

## Required fields

| field           | type   | notes                                                |
|-----------------|--------|------------------------------------------------------|
| schema_version  | string | constant `"1"`                                       |
| id              | string | UUID (idempotency key; deduped at the index layer)   |
| ts              | string | RFC3339 / RFC3339Nano timestamp                      |
| source_service  | string | service name e.g. `"mpc-signing-service"`            |
| actor_id        | string | user / service / key id                              |
| action          | string | e.g. `"mpc.sign"`, `"transaction.created"`          |
| target_type     | string | e.g. `"transaction"`, `"wallet"`, `"key"`           |
| target_id       | string | id of the target resource                            |
| payload_hash    | string | `sha256:<hex>` of the `payload` bytes (omit only if `payload` is absent) |
| payload         | object | optional JSON object; when present, `payload_hash` MUST match |

## Informational / optional fields (producers SHOULD omit)

| field      | notes                                                         |
|------------|---------------------------------------------------------------|
| prev_hash  | audit service sets from the chain head at insert time         |
| this_hash  | audit service recomputes from the indexed fields              |
| redaction  | optional redaction policy name applied by the producer        |

## Wire JSON example

```json
{
  "schema_version": "1",
  "id": "0192f0a1-2b3c-4d5e-6789-0abcdef12345",
  "ts": "2025-07-20T12:34:56.789012Z",
  "source_service": "mpc-signing-service",
  "actor_id": "node-0",
  "action": "mpc.sign",
  "target_type": "transaction",
  "target_id": "0xabc123",
  "payload_hash": "sha256:5d41402abc4b2a76b9719d911017c592...",
  "payload": {
    "signing_session_id": "sess-1",
    "key_id": "k1",
    "chain": "evm",
    "result": "signed"
  }
}
```

## Kafka routing

- topic: `audit.v1`
- key: any stable identifier per event ordering group (e.g. `target_id`,
  `event_id`). The audit service is a single-consumer group
  (`audit-event-log`); per-key ordering is preserved within a partition.
- value: the envelope serialized as JSON.