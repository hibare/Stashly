// Package s3 provides an implementation of storage interface for S3-compatible backends.
package s3

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	commonS3 "github.com/hibare/GoCommon/v2/pkg/aws/s3"
	"github.com/hibare/stashly/internal/config"
)

// S3 implements the StorageIface for S3-compatible storage backends.
type S3 struct {
	s3  commonS3.ClientIface
	cfg *config.Config
}

// Init prepares the S3 storage by establishing a session.
func (s *S3) Init(ctx context.Context) error {
	s3, err := commonS3.NewClient(ctx, commonS3.Options{
		Endpoint:  s.cfg.S3.Endpoint,
		Region:    s.cfg.S3.Region,
		AccessKey: s.cfg.S3.AccessKey,
		SecretKey: s.cfg.S3.SecretKey,
	})
	if err != nil {
		return err
	}

	s.s3 = s3

	return nil
}

// Name returns the name of the storage backend (e.g., "s3").
func (s *S3) Name() string {
	return fmt.Sprintf("s3 (%s)", s.cfg.S3.Bucket)
}

// Upload uploads a local file to S3 and returns the remote key/path.
func (s *S3) Upload(ctx context.Context, localPath string) (string, error) {
	prefix := s.s3.BuildTimestampedKey(s.cfg.S3.Prefix, s.cfg.App.InstanceID)

	slog.DebugContext(ctx, "Uploading file to S3", "file", localPath, "bucket", s.cfg.S3.Bucket, "key_prefix", prefix)
	key, err := s.s3.UploadFile(ctx, s.cfg.S3.Bucket, prefix, localPath)
	if err != nil {
		return "", err
	}
	return key, nil
}

// List returns keys/identifiers under the configured prefix.
func (s *S3) List(ctx context.Context) ([]string, error) {
	// Prefix excluding timestamp to list all backups for this instance
	prefix := s.s3.BuildKey(s.cfg.S3.Prefix, s.cfg.App.InstanceID)
	keys, err := s.s3.ListObjectsAtPrefix(ctx, s.cfg.S3.Bucket, prefix)
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// Delete deletes the provided key/path from S3 storage.
func (s *S3) Delete(ctx context.Context, timestamp string) error {
	prefix := s.s3.BuildKey(s.cfg.S3.Prefix, s.cfg.App.InstanceID)
	key := filepath.Join(prefix, timestamp)
	return s.s3.DeleteObjects(ctx, s.cfg.S3.Bucket, key, true)
}

// TrimPrefix trims the configured prefix from a given key, if present.
func (s *S3) TrimPrefix(keys []string) []string {
	// Trim the prefix from the keys to get timestamps only
	return s.s3.TrimPrefix(keys, s.s3.BuildKey(s.cfg.S3.Prefix, s.cfg.App.InstanceID))
}

// NewS3Storage creates a new S3Storage instance with the provided configuration.
func NewS3Storage(cfg *config.Config) *S3 {
	return &S3{
		cfg: cfg,
	}
}
