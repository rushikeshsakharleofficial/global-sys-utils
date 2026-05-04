package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/cloudutil"
	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/gcpclient"
)

const usage = `Usage: global-gcp-restore --source <gs://bucket[/prefix]> --destination <path> [OPTIONS]

Restore log files from Google Cloud Storage.

Options:
  --source       <gs://…>        GCS source URL (bucket and optional prefix)
  --destination  <path>          Local directory to restore into
  --pattern      <glob>          Filename glob to include (default: *)
  --exclude      <glob>          Filename glob to exclude; repeatable
  --parallel     <N>             Concurrent downloads (default: 4)
  --project      <id>            GCP project ID
  --credentials  <path>          Path to service account JSON key file
  --retries      <N>             Download retry count (default: 3)
  --flatten                      Put all files directly in destination (no subdirs)
  --dry-run                      Print actions without downloading anything
  -h, --help                     Show this help

Examples:
  global-gcp-restore --source gs://my-bucket/logs --destination /var/log/restore
  global-gcp-restore --source gs://my-bucket/logs/myhost --destination /tmp/restore \
      --pattern "*.gz" --flatten --dry-run
`

func main() {
	var (
		source      string
		destination string
		pattern     string
		excludeList cloudutil.MultiFlag
		parallel    int
		project     string
		credentials string
		retries     int
		flatten     bool
		dryRun      bool
		help        bool
	)

	fs := flag.NewFlagSet("global-gcp-restore", flag.ExitOnError)
	fs.StringVar(&source, "source", "", "")
	fs.StringVar(&destination, "destination", "", "")
	fs.StringVar(&pattern, "pattern", "*", "")
	fs.Var(&excludeList, "exclude", "")
	fs.IntVar(&parallel, "parallel", 4, "")
	fs.StringVar(&project, "project", "", "")
	fs.StringVar(&credentials, "credentials", "", "")
	fs.IntVar(&retries, "retries", 3, "")
	fs.BoolVar(&flatten, "flatten", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&help, "h", false, "")
	fs.BoolVar(&help, "help", false, "")
	fs.Usage = func() { fmt.Print(usage) }
	fs.Parse(os.Args[1:])

	if help || source == "" || destination == "" {
		fmt.Print(usage)
		os.Exit(0)
	}

	bucket, prefix, err := gcpclient.ParseGCSURL(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := gcpclient.NewClient(ctx, gcpclient.Config{Project: project, Credentials: credentials})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: create GCS client:", err)
		os.Exit(1)
	}
	defer client.Close()

	listPrefix := prefix
	if listPrefix != "" {
		listPrefix += "/"
	}
	fmt.Printf("Listing blobs under gs://%s/%s ...\n", bucket, prefix)
	blobs, err := client.ListBlobs(ctx, bucket, listPrefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: list blobs:", err)
		os.Exit(1)
	}

	var candidates []gcpclient.BlobInfo
	for _, b := range blobs {
		name := filepath.Base(b.Name)
		if !cloudutil.GlobMatch(name, pattern) {
			continue
		}
		excluded := false
		for _, ex := range excludeList {
			if cloudutil.GlobMatch(name, ex) || cloudutil.GlobMatch(b.Name, ex) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		candidates = append(candidates, b)
	}

	if len(candidates) == 0 {
		fmt.Println("No blobs found matching criteria.")
		os.Exit(0)
	}
	fmt.Printf("Found %d blob(s) to restore\n", len(candidates))

	if dryRun {
		for _, b := range candidates {
			dest := cloudutil.LocalPath(b.Name, prefix, destination, flatten)
			fmt.Printf("[DRY-RUN] Would download: gs://%s/%s -> %s\n", bucket, b.Name, dest)
		}
		fmt.Printf("\nTotal: %d blob(s)\n", len(candidates))
		return
	}

	if err := os.MkdirAll(destination, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: create destination:", err)
		os.Exit(1)
	}

	var (
		ok   int64
		fail int64
		wg   sync.WaitGroup
		sem  = make(chan struct{}, parallel)
	)

	for _, b := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(blob gcpclient.BlobInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			dest := cloudutil.LocalPath(blob.Name, prefix, destination, flatten)

			if _, err := os.Stat(dest); err == nil {
				fmt.Printf("Skipping (exists): %s\n", dest)
				atomic.AddInt64(&ok, 1)
				return
			}

			dlErr := client.Download(ctx, bucket, blob.Name, dest, gcpclient.DownloadOptions{Retries: retries})
			if dlErr != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", blob.Name, dlErr)
				atomic.AddInt64(&fail, 1)
				return
			}
			fmt.Printf("Restored: gs://%s/%s -> %s\n", bucket, blob.Name, dest)
			atomic.AddInt64(&ok, 1)
		}(b)
	}
	wg.Wait()

	fmt.Printf("\nDone: %d succeeded, %d failed\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}
