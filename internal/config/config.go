// Package config loads runtime configuration for the Audit Event Log
// service from environment variables. It exposes a single Config struct
// consumed by the composition root (internal/app). Defaults match the
// README configuration table.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the top-level app configuration loaded from env.
type Config struct {
	Port                     string
	KafkaBrokers             []string
	KafkaTopic               string
	KafkaConsumerGroup       string
	DBURL                    string
	PayloadBucket            string
	PayloadStorageClass      string
	GlacierTransitionDays    int
	DeepArchiveTransitionDays int
	RetentionDays            int
	ChainAnchorInterval      time.Duration
	KMSKeyID                 string
	ExternalNotaryURL        string
	RedactionPolicyPath      string
	LogLevel                 string
	LegalHoldDefault         bool
}

// Load reads configuration from the environment. Missing required values
// are tolerated so the binary still boots for local dev and tests; the
// composition root falls back to in-memory / fake implementations.
func Load() Config {
	return Config{
		Port:                      envOr("PORT", "8080"),
		KafkaBrokers:              splitCSV(os.Getenv("KAFKA_BROKERS")),
		KafkaTopic:                 envOr("KAFKA_TOPIC", "audit.v1"),
		KafkaConsumerGroup:        envOr("KAFKA_CONSUMER_GROUP", "audit-event-log"),
		DBURL:                     os.Getenv("DB_URL"),
		PayloadBucket:             os.Getenv("PAYLOAD_BUCKET"),
		PayloadStorageClass:       envOr("PAYLOAD_STORAGE_CLASS", "STANDARD"),
		GlacierTransitionDays:     envInt("GLACIER_TRANSITION_DAYS", 90),
		DeepArchiveTransitionDays: envInt("DEEP_ARCHIVE_TRANSITION_DAYS", 365),
		RetentionDays:             envInt("RETENTION_DAYS", 2555),
		ChainAnchorInterval:       envDur("CHAIN_ANCHOR_INTERVAL_MINUTES", 60*time.Minute),
		KMSKeyID:                  os.Getenv("KMS_KEY_ID"),
		ExternalNotaryURL:         os.Getenv("EXTERNAL_NOTARY_URL"),
		RedactionPolicyPath:       envOr("REDACTION_POLICY_PATH", "/etc/audit/redaction.yaml"),
		LogLevel:                  envOr("LOG_LEVEL", "info"),
		LegalHoldDefault:          envBool("LEGAL_HOLD_DEFAULT", false),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// Plain integer -> minutes (matches CHAIN_ANCHOR_INTERVAL_MINUTES).
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Minute
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}