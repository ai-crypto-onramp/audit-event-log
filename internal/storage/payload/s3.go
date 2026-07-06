package payload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Store is the AWS S3 implementation of Store + BucketProvisioner. It
// provisions a bucket with Object Lock enabled (compliance mode, retention =
// cfg.Retention), applies the lifecycle policy, and writes payloads with that
// retention.
//
// For local development against an S3-compatible store (e.g. LocalStack or
// MinIO) pass EndpointURL and a static credential pair; the constructor wires
// up the BaseEndpoint and credentials provider accordingly.
type S3Store struct {
	cfg  Config
	api  *s3.Client
	life Lifecycle
}

// S3Options carries optional local-dev settings for the S3 client.
type S3Options struct {
	Region       string
	EndpointURL  string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

// NewS3Store constructs an S3Store. The bucket is not provisioned until
// Provision is called.
func NewS3Store(cfg Config, opts S3Options) (*S3Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	awsCfg, err := loadAWSConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("payload: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if opts.UsePathStyle {
			o.UsePathStyle = true
		}
	})
	return &S3Store{
		cfg:  cfg,
		api:  client,
		life: Lifecycle{cfg.GlacierTransitionDays, cfg.DeepArchiveTransitionDays},
	}, nil
}

func loadAWSConfig(opts S3Options) (aws.Config, error) {
	region := opts.Region
	if region == "" {
		region = "us-east-1"
	}
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	if opts.EndpointURL != "" || opts.AccessKey != "" {
		loaders = append(loaders, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, "")))
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(), loaders...)
	if err != nil {
		return awsCfg, err
	}
	if opts.EndpointURL != "" {
		awsCfg.BaseEndpoint = aws.String(opts.EndpointURL)
	}
	return awsCfg, nil
}

// Lifecycle returns the lifecycle policy that Provision applies.
func (s *S3Store) Lifecycle() Lifecycle { return s.life }

// Provision creates the bucket with Object Lock enabled and applies the
// lifecycle policy. It is idempotent: if the bucket already exists it moves
// on to the retention + lifecycle steps.
func (s *S3Store) Provision(ctx context.Context) error {
	if ctx == nil {
		return errors.New("payload: nil context")
	}
	_, err := s.api.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                     aws.String(s.cfg.Bucket),
		ObjectLockEnabledForBucket: aws.Bool(true),
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("payload: create bucket: %w", err)
	}

	// Object Lock requires versioning; enable it explicitly.
	if _, err := s.api.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(s.cfg.Bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	}); err != nil {
		return fmt.Errorf("payload: enable versioning: %w", err)
	}

	// Default retention for new objects in compliance mode.
	days := int32(s.cfg.Retention / (24 * time.Hour))
	if days <= 0 {
		days = 1
	}
	if _, err := s.api.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(s.cfg.Bucket),
		ObjectLockConfiguration: &types.ObjectLockConfiguration{
			ObjectLockEnabled: types.ObjectLockEnabledEnabled,
			Rule: &types.ObjectLockRule{
				DefaultRetention: &types.DefaultRetention{
					Mode: types.ObjectLockRetentionModeCompliance,
					Days: aws.Int32(days),
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("payload: set object lock: %w", err)
	}

	// Lifecycle: Standard -> Glacier -> Deep Archive.
	if _, err := s.api.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(s.cfg.Bucket),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: []types.LifecycleRule{
				{
					ID:     aws.String("audit-payload-tiering"),
					Status: types.ExpirationStatusEnabled,
					Filter: &types.LifecycleRuleFilter{
						Prefix: aws.String(""),
					},
					Transitions: []types.Transition{
						{
							Days:         aws.Int32(int32(s.cfg.GlacierTransitionDays)),
							StorageClass: types.TransitionStorageClassGlacier,
						},
						{
							Days:         aws.Int32(int32(s.cfg.DeepArchiveTransitionDays)),
							StorageClass: types.TransitionStorageClassDeepArchive,
						},
					},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("payload: set lifecycle: %w", err)
	}
	return nil
}

// Put writes an object with the configured retention in compliance mode. S3
// itself rejects any overwrite attempt on a retained object with an error
// equivalent to ErrObjectLocked.
func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	if ctx == nil {
		return errors.New("payload: nil context")
	}
	if key == "" {
		return errors.New("payload: empty key")
	}
	sc := opts.StorageClass
	if sc == "" {
		sc = s.cfg.StorageClass
	}
	_, err := s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.cfg.Bucket),
		Key:          aws.String(key),
		Body:         body,
		StorageClass: types.StorageClass(sc),
	})
	if err != nil {
		return fmt.Errorf("payload: put %s: %w", key, err)
	}
	retention := opts.Retention
	if retention <= 0 {
		retention = s.cfg.Retention
	}
	if _, err := s.api.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
		Retention: &types.ObjectLockRetention{
			Mode:            types.ObjectLockRetentionModeCompliance,
			RetainUntilDate: aws.Time(time.Now().UTC().Add(retention)),
		},
	}); err != nil {
		return fmt.Errorf("payload: set retention %s: %w", key, err)
	}
	return nil
}

// Get fetches an object's body.
func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("payload: nil context")
	}
	out, err := s.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("payload: get %s: %w", key, err)
	}
	return out.Body, nil
}

// isAlreadyExists returns true for BucketAlreadyOwnedByYou / BucketAlreadyExists.
func isAlreadyExists(err error) bool {
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	return errors.As(err, &owned) || errors.As(err, &exists)
}