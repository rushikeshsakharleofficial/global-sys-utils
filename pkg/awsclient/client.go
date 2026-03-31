// Package awsclient wraps the AWS SDK v2 S3 client with upload, download,
// list, and delete operations — modelled after boto3's S3 client interface.
package awsclient

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds options for creating an S3 client.
type Config struct {
	Profile string
	Region  string
}

// Client wraps the AWS S3 client.
type Client struct {
	s3 *s3.Client
}

// ObjectInfo describes an S3 object.
type ObjectInfo struct {
	Key  string
	Size int64
}

// UploadOptions controls upload behaviour.
type UploadOptions struct {
	Verify  bool // compare local MD5 with ETag after upload
	Retries int  // attempts before giving up (min 1)
}

// DownloadOptions controls download behaviour.
type DownloadOptions struct {
	Retries int
}

// NewClient creates an S3 client using the default credential chain
// (env vars → ~/.aws/credentials → IAM role), with optional profile and region overrides.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	var opts []func(*config.LoadOptions) error
	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}
	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &Client{s3: s3.NewFromConfig(awsCfg)}, nil
}

// Upload uploads localPath to s3://bucket/key.
// If opts.Verify is true, compares the local MD5 against the returned ETag
// (works for single-part uploads; multipart ETags contain '-' and are skipped).
func (c *Client) Upload(ctx context.Context, localPath, bucket, key string, opts UploadOptions) error {
	retries := max1(opts.Retries)

	var localMD5 string
	if opts.Verify {
		var err error
		localMD5, err = md5Hex(localPath)
		if err != nil {
			return fmt.Errorf("md5 %s: %w", localPath, err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}
		if err := c.putObject(ctx, localPath, bucket, key); err != nil {
			lastErr = err
			continue
		}
		if opts.Verify {
			if err := c.verifyETag(ctx, bucket, key, localMD5); err != nil {
				lastErr = err
				continue
			}
		}
		return nil
	}
	return fmt.Errorf("upload s3://%s/%s failed after %d attempt(s): %w", bucket, key, retries, lastErr)
}

func (c *Client) putObject(ctx context.Context, localPath, bucket, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(stat.Size()),
	})
	return err
}

func (c *Client) verifyETag(ctx context.Context, bucket, key, localMD5 string) error {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("head object: %w", err)
	}
	etag := strings.Trim(aws.ToString(out.ETag), `"`)
	// Multipart upload ETags contain '-'; skip comparison for those.
	if !strings.Contains(etag, "-") && etag != localMD5 {
		return fmt.Errorf("checksum mismatch: local=%s s3=%s", localMD5, etag)
	}
	return nil
}

// Download downloads s3://bucket/key to localPath, creating parent directories as needed.
func (c *Client) Download(ctx context.Context, bucket, key, localPath string, opts DownloadOptions) error {
	retries := max1(opts.Retries)

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}
		if err := c.getObject(ctx, bucket, key, localPath); err != nil {
			os.Remove(localPath)
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("download s3://%s/%s failed after %d attempt(s): %w", bucket, key, retries, lastErr)
}

func (c *Client) getObject(ctx context.Context, bucket, key, localPath string) error {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	defer out.Body.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, out.Body)
	return err
}

// ListObjects returns all objects whose key starts with prefix inside bucket.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	var out []ObjectInfo
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			out = append(out, ObjectInfo{
				Key:  aws.ToString(obj.Key),
				Size: aws.ToInt64(obj.Size),
			})
		}
	}
	return out, nil
}

// Delete removes an object from S3.
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// md5Hex computes the MD5 of a file and returns it as a lowercase hex string.
func md5Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
