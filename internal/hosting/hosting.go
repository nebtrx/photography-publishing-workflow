// Package hosting provides an interface and implementations for temporary
// image hosting. Instagram Graph API requires publicly accessible image URLs.
package hosting

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Host is the interface for temporary image hosting.
type Host interface {
	// Upload uploads a local file and returns its public URL.
	Upload(ctx context.Context, key string, filePath string) (publicURL string, err error)

	// Delete removes a previously uploaded object.
	Delete(ctx context.Context, key string) error
}

// R2Config holds Cloudflare R2 connection parameters.
type R2Config struct {
	AccessKeyID     string
	SecretAccessKey  string
	Bucket          string
	Endpoint        string // e.g. https://<account-id>.r2.cloudflarestorage.com
	PublicURL       string // e.g. https://pub-xxx.r2.dev
}

// R2ConfigFromEnv loads R2 config from environment variables.
func R2ConfigFromEnv() (*R2Config, error) {
	cfg := &R2Config{
		AccessKeyID:    os.Getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		Bucket:         os.Getenv("R2_BUCKET"),
		Endpoint:       os.Getenv("R2_ENDPOINT"),
		PublicURL:      os.Getenv("R2_PUBLIC_URL"),
	}

	var missing []string
	if cfg.AccessKeyID == "" {
		missing = append(missing, "R2_ACCESS_KEY_ID")
	}
	if cfg.SecretAccessKey == "" {
		missing = append(missing, "R2_SECRET_ACCESS_KEY")
	}
	if cfg.Bucket == "" {
		missing = append(missing, "R2_BUCKET")
	}
	if cfg.Endpoint == "" {
		missing = append(missing, "R2_ENDPOINT")
	}
	if cfg.PublicURL == "" {
		missing = append(missing, "R2_PUBLIC_URL")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// R2 implements Host using Cloudflare R2 (S3-compatible).
type R2 struct {
	client    *s3.Client
	bucket    string
	publicURL string
}

// NewR2 creates an R2 host from the given config.
func NewR2(ctx context.Context, cfg *R2Config) (*R2, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{URL: cfg.Endpoint}, nil
		},
	)

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(resolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &R2{
		client:    client,
		bucket:    cfg.Bucket,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}, nil
}

// Upload uploads a local file to R2 and returns the public URL.
func (r *R2) Upload(ctx context.Context, key string, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	contentType := "image/jpeg"

	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload %s to R2: %w", key, err)
	}

	publicURL := fmt.Sprintf("%s/%s", r.publicURL, key)
	return publicURL, nil
}

// Delete removes an object from R2.
func (r *R2) Delete(ctx context.Context, key string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete %s from R2: %w", key, err)
	}
	return nil
}

// DryRunHost is a no-op host for dry-run mode that logs operations.
type DryRunHost struct{}

func (d *DryRunHost) Upload(_ context.Context, key, filePath string) (string, error) {
	url := fmt.Sprintf("https://dry-run.r2.dev/%s", key)
	fmt.Printf("[DRY RUN] Would upload %s → %s\n", filePath, url)
	return url, nil
}

func (d *DryRunHost) Delete(_ context.Context, key string) error {
	fmt.Printf("[DRY RUN] Would delete %s from R2\n", key)
	return nil
}

// MemoryHost is an in-memory host for testing.
type MemoryHost struct {
	Uploaded map[string]string // key → filePath
	Deleted  []string
}

func NewMemoryHost() *MemoryHost {
	return &MemoryHost{Uploaded: map[string]string{}}
}

func (m *MemoryHost) Upload(_ context.Context, key, filePath string) (string, error) {
	m.Uploaded[key] = filePath
	return fmt.Sprintf("https://test.r2.dev/%s", key), nil
}

func (m *MemoryHost) Delete(_ context.Context, key string) error {
	m.Deleted = append(m.Deleted, key)
	return nil
}

// Ensure interfaces are satisfied.
var (
	_ Host = (*R2)(nil)
	_ Host = (*DryRunHost)(nil)
	_ Host = (*MemoryHost)(nil)
	_ io.Reader // suppress unused import if needed
)
