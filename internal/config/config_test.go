package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear required env so we exercise the defaults path.
	for _, k := range []string{"KAFKA_BROKERS", "DB_URL", "PAYLOAD_BUCKET", "KMS_KEY_ID"} {
		_ = os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("Port: %q", cfg.Port)
	}
	if cfg.KafkaTopic != "audit.v1" {
		t.Errorf("KafkaTopic: %q", cfg.KafkaTopic)
	}
	if cfg.KafkaConsumerGroup != "audit-event-log" {
		t.Errorf("KafkaConsumerGroup: %q", cfg.KafkaConsumerGroup)
	}
	if cfg.PayloadStorageClass != "STANDARD" {
		t.Errorf("PayloadStorageClass: %q", cfg.PayloadStorageClass)
	}
	if cfg.GlacierTransitionDays != 90 {
		t.Errorf("GlacierTransitionDays: %d", cfg.GlacierTransitionDays)
	}
	if cfg.DeepArchiveTransitionDays != 365 {
		t.Errorf("DeepArchiveTransitionDays: %d", cfg.DeepArchiveTransitionDays)
	}
	if cfg.RetentionDays != 2555 {
		t.Errorf("RetentionDays: %d", cfg.RetentionDays)
	}
	if cfg.ChainAnchorInterval != 60*time.Minute {
		t.Errorf("ChainAnchorInterval: %v", cfg.ChainAnchorInterval)
	}
	if cfg.LegalHoldDefault {
		t.Error("LegalHoldDefault should default false")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: %q", cfg.LogLevel)
	}
	if cfg.RedactionPolicyPath != "/etc/audit/redaction.yaml" {
		t.Errorf("RedactionPolicyPath: %q", cfg.RedactionPolicyPath)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("KAFKA_BROKERS", "a:9092, b:9092 , c:9092")
	t.Setenv("KAFKA_TOPIC", "audit.v2")
	t.Setenv("RETENTION_DAYS", "3000")
	t.Setenv("GLACIER_TRANSITION_DAYS", "30")
	t.Setenv("DEEP_ARCHIVE_TRANSITION_DAYS", "120")
	t.Setenv("CHAIN_ANCHOR_INTERVAL_MINUTES", "15m")
	t.Setenv("LEGAL_HOLD_DEFAULT", "true")
	t.Setenv("DB_URL", "postgres://localhost/audit")
	t.Setenv("PAYLOAD_BUCKET", "audit-bucket")
	t.Setenv("KMS_KEY_ID", "alias/audit")

	cfg := Load()
	if cfg.Port != "9090" {
		t.Errorf("Port: %q", cfg.Port)
	}
	if len(cfg.KafkaBrokers) != 3 || cfg.KafkaBrokers[0] != "a:9092" || cfg.KafkaBrokers[1] != "b:9092" || cfg.KafkaBrokers[2] != "c:9092" {
		t.Errorf("KafkaBrokers: %v", cfg.KafkaBrokers)
	}
	if cfg.KafkaTopic != "audit.v2" {
		t.Errorf("KafkaTopic: %q", cfg.KafkaTopic)
	}
	if cfg.RetentionDays != 3000 {
		t.Errorf("RetentionDays: %d", cfg.RetentionDays)
	}
	if cfg.ChainAnchorInterval != 15*time.Minute {
		t.Errorf("ChainAnchorInterval: %v", cfg.ChainAnchorInterval)
	}
	if !cfg.LegalHoldDefault {
		t.Error("LegalHoldDefault should be true")
	}
	if cfg.DBURL != "postgres://localhost/audit" {
		t.Errorf("DBURL: %q", cfg.DBURL)
	}
}

func TestEnvHelpers(t *testing.T) {
	if envOr("NOPE_XYZ", "def") != "def" {
		t.Error("envOr default")
	}
	if envInt("NOPE_XYZ", 42) != 42 {
		t.Error("envInt default")
	}
	if envDur("NOPE_XYZ", 5*time.Second) != 5*time.Second {
		t.Error("envDur default")
	}
	if envBool("NOPE_XYZ", true) != true {
		t.Error("envBool default")
	}
	t.Setenv("CHAIN_ANCHOR_INTERVAL_MINUTES", "10")
	if envDur("CHAIN_ANCHOR_INTERVAL_MINUTES", time.Hour) != 10*time.Minute {
		t.Error("envDur minutes fallback")
	}
	if splitCSV("") != nil {
		t.Error("splitCSV empty")
	}
}