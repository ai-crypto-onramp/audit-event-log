package config

import (
	"os"
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	for _, k := range []string{
		"PORT", "KAFKA_BROKERS", "KAFKA_TOPIC", "KAFKA_CONSUMER_GROUP", "DB_URL",
		"PAYLOAD_BUCKET", "PAYLOAD_STORAGE_CLASS", "GLACIER_TRANSITION_DAYS",
		"DEEP_ARCHIVE_TRANSITION_DAYS", "RETENTION_DAYS",
		"CHAIN_ANCHOR_INTERVAL_MINUTES", "KMS_KEY_ID", "EXTERNAL_NOTARY_URL",
		"REDACTION_POLICY_PATH", "LOG_LEVEL", "LEGAL_HOLD_DEFAULT",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("DB_URL", "postgres://u:p@localhost/db")
	t.Setenv("PAYLOAD_BUCKET", "audit-payloads")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %q want %q", cfg.Port, DefaultPort)
	}
	if cfg.KafkaTopic != DefaultKafkaTopic {
		t.Errorf("KafkaTopic = %q want %q", cfg.KafkaTopic, DefaultKafkaTopic)
	}
	if cfg.KafkaConsumerGroup != DefaultKafkaConsumerGroup {
		t.Errorf("KafkaConsumerGroup = %q want %q", cfg.KafkaConsumerGroup, DefaultKafkaConsumerGroup)
	}
	if cfg.PayloadStorageClass != DefaultPayloadStorageClass {
		t.Errorf("PayloadStorageClass = %q want %q", cfg.PayloadStorageClass, DefaultPayloadStorageClass)
	}
	if cfg.GlacierTransitionDays != DefaultGlacierTransitionDays {
		t.Errorf("GlacierTransitionDays = %d want %d", cfg.GlacierTransitionDays, DefaultGlacierTransitionDays)
	}
	if cfg.DeepArchiveTransitionDays != DefaultDeepArchiveTransitionDays {
		t.Errorf("DeepArchiveTransitionDays = %d want %d", cfg.DeepArchiveTransitionDays, DefaultDeepArchiveTransitionDays)
	}
	if cfg.RetentionDays != DefaultRetentionDays {
		t.Errorf("RetentionDays = %d want %d", cfg.RetentionDays, DefaultRetentionDays)
	}
	if cfg.ChainAnchorIntervalMinutes != DefaultChainAnchorIntervalMinutes {
		t.Errorf("ChainAnchorIntervalMinutes = %d want %d", cfg.ChainAnchorIntervalMinutes, DefaultChainAnchorIntervalMinutes)
	}
	if cfg.RedactionPolicyPath != DefaultRedactionPolicyPath {
		t.Errorf("RedactionPolicyPath = %q want %q", cfg.RedactionPolicyPath, DefaultRedactionPolicyPath)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.LegalHoldDefault {
		t.Errorf("LegalHoldDefault = true want false")
	}
	if want := time.Duration(DefaultRetentionDays) * 24 * time.Hour; cfg.RetentionDuration() != want {
		t.Errorf("RetentionDuration = %v want %v", cfg.RetentionDuration(), want)
	}
}

func TestFromEnvMissingRequired(t *testing.T) {
	for _, k := range []string{"DB_URL", "PAYLOAD_BUCKET", "KAFKA_BROKERS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	_, err := FromEnv()
	if err == nil {
		t.Fatalf("expected error for missing required env vars")
	}
}

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":           nil,
		"a":          {"a"},
		"a,b,c":      {"a", "b", "c"},
		" a , b ,c":  {"a", "b", "c"},
	}
	for in, want := range cases {
		got := splitCSV(in)
		if len(got) != len(want) {
			t.Errorf("splitCSV(%q) = %v want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestEnvIntOrInvalid(t *testing.T) {
	t.Setenv("GLACIER_TRANSITION_DAYS", "not-a-number")
	if got := envIntOr("GLACIER_TRANSITION_DAYS", 42); got != 42 {
		t.Errorf("envIntOr invalid = %d want 42", got)
	}
}

func TestEnvBoolOr(t *testing.T) {
	t.Setenv("LEGAL_HOLD_DEFAULT", "true")
	if !envBoolOr("LEGAL_HOLD_DEFAULT", false) {
		t.Errorf("envBoolOr true = false want true")
	}
	t.Setenv("LEGAL_HOLD_DEFAULT", "garbage")
	if envBoolOr("LEGAL_HOLD_DEFAULT", true) != true {
		t.Errorf("envBoolOr garbage should fall back to default")
	}
}