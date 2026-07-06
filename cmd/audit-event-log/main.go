// Command audit-event-log is the service entry point. Stage 1 implements the
// migration runner and idempotent schema bootstrap: it loads config from the
// environment, applies the PostgreSQL migrations, and provisions the S3
// payload bucket with Object Lock and the Glacier lifecycle.
//
// Usage:
//
//	audit-event-log migrate      Apply DB migrations and provision S3 bucket.
//	audit-event-log serve        Run the HTTP server (future stages).
//
// The migrate subcommand is idempotent and safe to run on every boot.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-crypto-onramp/audit-event-log/internal/config"
	"github.com/ai-crypto-onramp/audit-event-log/internal/storage/payload"
	"github.com/ai-crypto-onramp/audit-event-log/internal/storage/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "audit-event-log:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("missing subcommand (migrate|serve)")
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch args[0] {
	case "migrate":
		return runMigrate(ctx, cfg)
	case "serve":
		return runServe(ctx, cfg)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runMigrate applies the PostgreSQL schema and provisions the S3 payload bucket.
// It is the idempotent schema bootstrap referenced by the Stage 1 acceptance
// criteria; running it on every boot is safe.
func runMigrate(ctx context.Context, cfg config.Config) error {
	log.Printf("migrate: connecting to postgres")
	poolCfg, err := pgxpool.ParseConfig(cfg.DBURL)
	if err != nil {
		return fmt.Errorf("parse DB_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	migrateCtx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := postgres.Run(migrateCtx, pool); err != nil {
		return fmt.Errorf("postgres migrations: %w", err)
	}
	if err := postgres.VerifySchema(migrateCtx, pool); err != nil {
		return fmt.Errorf("postgres schema verification: %w", err)
	}
	log.Printf("migrate: postgres schema OK (%d columns, %d indexes)",
		len(postgres.ExpectColumns), len(postgres.ExpectIndexes))

	log.Printf("migrate: provisioning S3 bucket %s", cfg.PayloadBucket)
	pcfg := payload.Config{
		Bucket:                    cfg.PayloadBucket,
		StorageClass:              cfg.PayloadStorageClass,
		Retention:                 cfg.RetentionDuration(),
		GlacierTransitionDays:     cfg.GlacierTransitionDays,
		DeepArchiveTransitionDays: cfg.DeepArchiveTransitionDays,
	}
	if err := pcfg.Validate(); err != nil {
		return fmt.Errorf("payload config: %w", err)
	}
	store, err := payload.NewS3Store(pcfg, payload.S3Options{
		Region:       os.Getenv("AWS_REGION"),
		EndpointURL:  os.Getenv("S3_ENDPOINT_URL"),
		AccessKey:    os.Getenv("S3_ACCESS_KEY_ID"),
		SecretKey:    os.Getenv("S3_SECRET_ACCESS_KEY"),
		UsePathStyle: os.Getenv("S3_USE_PATH_STYLE") == "true",
	})
	if err != nil {
		return fmt.Errorf("new s3 store: %w", err)
	}
	provCtx, cancel2 := withTimeout(ctx, 30*time.Second)
	defer cancel2()
	if err := store.Provision(provCtx); err != nil {
		return fmt.Errorf("provision s3: %w", err)
	}
	log.Printf("migrate: S3 bucket %s provisioned, lifecycle=%s",
		cfg.PayloadBucket, store.Lifecycle())
	return nil
}

// runServe is a stub for subsequent stages; Stage 1 only ships migrate.
func runServe(ctx context.Context, cfg config.Config) error {
	log.Printf("serve: not implemented in stage 1; use `audit-event-log migrate`")
	return errors.New("serve not implemented")
}

func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, d)
}