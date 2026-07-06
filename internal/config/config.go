// Package config loads runtime configuration for the audit-event-log service
// from environment variables, applying the defaults documented in the README.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration.
type Config struct {
	Port                     string
	KafkaBrokers            []string
	KafkaTopic              string
	KafkaConsumerGroup      string
	DBURL                   string
	PayloadBucket           string
	PayloadStorageClass     string
	GlacierTransitionDays   int
	DeepArchiveTransitionDays int
	RetentionDays           int
	ChainAnchorIntervalMinutes int
	KMSKeyID                string
	ExternalNotaryURL       string
	RedactionPolicyPath     string
	LogLevel                string
	LegalHoldDefault        bool
}

// Defaults mirror the README "Configuration" table.
const (
	DefaultPort                     = "8080"
	DefaultKafkaTopic               = "audit.v1"
	DefaultKafkaConsumerGroup       = "audit-event-log"
	DefaultPayloadStorageClass      = "STANDARD"
	DefaultGlacierTransitionDays    = 90
	DefaultDeepArchiveTransitionDays = 365
	DefaultRetentionDays            = 2555
	DefaultChainAnchorIntervalMinutes = 60
	DefaultRedactionPolicyPath      = "/etc/audit/redaction.yaml"
	DefaultLogLevel                 = "info"
)

// FromEnv reads configuration from the process environment, applying defaults
// for optional variables. Required variables that are absent return an error
// listing every missing entry so callers can surface them in one shot.
func FromEnv() (Config, error) {
	cfg := Config{
		Port:                       envOr("PORT", DefaultPort),
		KafkaBrokers:               splitCSV(os.Getenv("KAFKA_BROKERS")),
		KafkaTopic:                 envOr("KAFKA_TOPIC", DefaultKafkaTopic),
		KafkaConsumerGroup:         envOr("KAFKA_CONSUMER_GROUP", DefaultKafkaConsumerGroup),
		DBURL:                      os.Getenv("DB_URL"),
		PayloadBucket:              os.Getenv("PAYLOAD_BUCKET"),
		PayloadStorageClass:        envOr("PAYLOAD_STORAGE_CLASS", DefaultPayloadStorageClass),
		GlacierTransitionDays:      envIntOr("GLACIER_TRANSITION_DAYS", DefaultGlacierTransitionDays),
		DeepArchiveTransitionDays:  envIntOr("DEEP_ARCHIVE_TRANSITION_DAYS", DefaultDeepArchiveTransitionDays),
		RetentionDays:              envIntOr("RETENTION_DAYS", DefaultRetentionDays),
		ChainAnchorIntervalMinutes: envIntOr("CHAIN_ANCHOR_INTERVAL_MINUTES", DefaultChainAnchorIntervalMinutes),
		KMSKeyID:                   os.Getenv("KMS_KEY_ID"),
		ExternalNotaryURL:          os.Getenv("EXTERNAL_NOTARY_URL"),
		RedactionPolicyPath:        envOr("REDACTION_POLICY_PATH", DefaultRedactionPolicyPath),
		LogLevel:                   envOr("LOG_LEVEL", DefaultLogLevel),
		LegalHoldDefault:           envBoolOr("LEGAL_HOLD_DEFAULT", false),
	}

	var missing []string
	if cfg.DBURL == "" {
		missing = append(missing, "DB_URL")
	}
	if cfg.PayloadBucket == "" {
		missing = append(missing, "PAYLOAD_BUCKET")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBoolOr(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// RetentionDuration returns RETENTION_DAYS as a duration for Object Lock.
func (c Config) RetentionDuration() time.Duration {
	return time.Duration(c.RetentionDays) * 24 * time.Hour
}