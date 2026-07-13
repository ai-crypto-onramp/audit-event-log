// Package metrics registers the Prometheus collectors surfaced by the
// audit event log service: ingest throughput, latency, dedup count,
// rejection count, and verification outcomes. The package exposes
// pre-declared Collector objects so handlers can increment them without
// additional wiring.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// EventsIngested counts events successfully ingested (deduped at the
	// repository layer; each accepted event increments by 1).
	EventsIngested = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "events_ingested_total",
		Help:      "Total number of events successfully ingested.",
	}, []string{"service"})

	// IngestDuplicates counts events that were deduped on event id.
	IngestDuplicates = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "ingest_duplicates_total",
		Help:      "Total number of duplicate events dropped at ingest.",
	}, []string{"service"})

	// IngestRejections counts events rejected for validation failures.
	IngestRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "ingest_rejections_total",
		Help:      "Total number of events rejected at ingest (validation / decode failures).",
	}, []string{"service", "reason"})

	// IngestLatency observes ingest latency in seconds.
	IngestLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "audit_event_log",
		Name:      "ingest_duration_seconds",
		Help:      "Ingest latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"service"})

	// QueryLatency observes read API latency in seconds.
	QueryLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "audit_event_log",
		Name:      "query_duration_seconds",
		Help:      "Read query latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"endpoint"})

	// ChainAnchors counts anchor jobs run and their outcomes.
	ChainAnchors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "chain_anchors_total",
		Help:      "Total number of chain anchor jobs run.",
	}, []string{"outcome"})

	// ChainVerifyOutcomes counts single-event verification outcomes.
	ChainVerifyOutcomes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "chain_verify_outcomes_total",
		Help:      "Total number of chain verification outcomes.",
	}, []string{"status"})

	// ExportsCreated counts export jobs created.
	ExportsCreated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "exports_created_total",
		Help:      "Total number of export jobs created.",
	}, []string{"format"})

	// ExportsCompleted counts export jobs completed.
	ExportsCompleted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "exports_completed_total",
		Help:      "Total number of export jobs completed.",
	}, []string{"format", "outcome"})

	// DeadLettered counts messages routed to the dead-letter sink.
	DeadLettered = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "audit_event_log",
		Name:      "dead_lettered_total",
		Help:      "Total number of messages routed to the dead-letter sink.",
	}, []string{"topic"})
)

var registerOnce sync.Once

// Register registers all collectors on the provided Registerer. If reg is
// nil, prometheus.DefaultRegisterer is used. Safe to call multiple times;
// only the first call has effect.
func Register(reg prometheus.Registerer) {
	registerOnce.Do(func() {
		if reg == nil {
			reg = prometheus.DefaultRegisterer
		}
		reg.MustRegister(
			EventsIngested,
			IngestDuplicates,
			IngestRejections,
			IngestLatency,
			QueryLatency,
			ChainAnchors,
			ChainVerifyOutcomes,
			ExportsCreated,
			ExportsCompleted,
			DeadLettered,
		)
	})
}

func init() {
	Register(prometheus.DefaultRegisterer)
}