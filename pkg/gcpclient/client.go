// Package gcpclient wraps the Google Cloud Storage Go client with upload,
// download, list, and delete operations — modelled after the google-cloud-storage
// Python client interface.
package gcpclient

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// Config holds options for creating a GCS client.
type Config struct {
	Project     string // GCP project ID (optional; inferred from ADC if empty)
	Credentials string // path to service account JSON; empty = use ADC
}

// Client wraps the GCS client.
type Client struct {
	gcs *storage.Client
}

// BlobInfo describes a GCS blob.
type BlobInfo struct {
	Name string
	Size int64
}

// UploadOptions controls upload behaviour.
type UploadOptions struct {
	Verify  bool // compare local MD5 against GCS-stored MD5 after upload
	Retries int
}

// DownloadOptions controls download behaviour.
type DownloadOptions struct {
	Retries int
}

// NewClient creates a GCS client.
// If cfg.Credentials is set, loads the service account key from that file.
// Otherwise uses Application Default Credentials (ADC).
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	var opts []option.ClientOption
	if cfg.Credentials != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.Credentials))
	}
	if cfg.Project != "" {
		opts = append(opts, option.WithQuotaProject(cfg.Project))
	}
	gcs, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	return &Client{gcs: gcs}, nil
}

// Close releases the underlying GCS client connection.
func (c *Client) Close() {
	c.gcs.Close()
}

// Upload uploads localPath to gs://bucket/blobName.
// If opts.Verify is true, the local MD5 is sent in the request so GCS validates
// it server-side, then we confirm it matches the stored object attrs.
func (c *Client) Upload(ctx context.Context, localPath, bucket, blobName string, opts UploadOptions) error {
	retries := max1(opts.Retries)

	var localMD5b64 string
	if opts.Verify {
		var err error
		localMD5b64, err = md5B64(localPath)
		if err != nil {
			return fmt.Errorf("md5 %s: %w", localPath, err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}
		if err := c.writeBlob(ctx, localPath, bucket, blobName, localMD5b64, opts.Verify); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("upload gs://%s/%s failed after %d attempt(s): %w", bucket, blobName, retries, lastErr)
}

func (c *Client) writeBlob(ctx context.Context, localPath, bucket, blobName, localMD5b64 string, verify bool) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	obj := c.gcs.Bucket(bucket).Object(blobName)
	w := obj.NewWriter(ctx)

	if verify {
		// Decode base64 MD5 back to raw bytes for the writer field.
		raw, err := base64.StdEncoding.DecodeString(localMD5b64)
		if err == nil {
			w.MD5 = raw // GCS will reject the upload if the MD5 doesn't match
		}
	}

	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	if verify {
		attrs, err := obj.Attrs(ctx)
		if err != nil {
			return fmt.Errorf("get attrs: %w", err)
		}
		gcsMD5 := base64.StdEncoding.EncodeToString(attrs.MD5)
		if gcsMD5 != localMD5b64 {
			return fmt.Errorf("checksum mismatch: local=%s gcs=%s", localMD5b64, gcsMD5)
		}
	}
	return nil
}

// Download downloads gs://bucket/blobName to localPath, creating parent dirs as needed.
func (c *Client) Download(ctx context.Context, bucket, blobName, localPath string, opts DownloadOptions) error {
	retries := max1(opts.Retries)

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}
		if err := c.readBlob(ctx, bucket, blobName, localPath); err != nil {
			os.Remove(localPath)
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("download gs://%s/%s failed after %d attempt(s): %w", bucket, blobName, retries, lastErr)
}

func (c *Client) readBlob(ctx context.Context, bucket, blobName, localPath string) error {
	r, err := c.gcs.Bucket(bucket).Object(blobName).NewReader(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	return err
}

// ListBlobs returns all blobs whose name starts with prefix inside bucket.
func (c *Client) ListBlobs(ctx context.Context, bucket, prefix string) ([]BlobInfo, error) {
	it := c.gcs.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})

	var out []BlobInfo
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, BlobInfo{Name: attrs.Name, Size: attrs.Size})
	}
	return out, nil
}

// Delete removes a blob from GCS.
func (c *Client) Delete(ctx context.Context, bucket, blobName string) error {
	return c.gcs.Bucket(bucket).Object(blobName).Delete(ctx)
}

// md5B64 computes the MD5 of a file and returns it base64-encoded (GCS format).
func md5B64(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
