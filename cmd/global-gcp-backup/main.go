package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/cloudutil"
	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/gcpclient"
)

const usage = `Usage: global-gcp-backup --source <path> --destination <gs://bucket[/prefix]> --days <N> [OPTIONS]

Backup log files to Google Cloud Storage.

Options:
  --source       <path>          Source directory to scan
  --destination  <gs://…>        GCS destination URL
  --days         <N>             Process files whose name contains a date older than N days
  --pattern      <glob>          Filename glob to include (default: *)
  --exclude      <glob>          Filename glob to exclude; repeatable
  --parallel     <N>             Concurrent uploads (default: 4)
  --project      <id>            GCP project ID
  --credentials  <path>          Path to service account JSON key file
  --retries      <N>             Upload retry count (default: 3)
  --copy                         Copy files instead of moving (preserve source)
  --no-verify                    Skip MD5 checksum verification after upload
  --dry-run                      Print actions without making any changes
  -h, --help                     Show this help

Examples:
  global-gcp-backup --source /var/log/apps --destination gs://my-bucket/logs --days 30
  global-gcp-backup --source /var/log/apps --destination gs://my-bucket/logs --days 30 \
      --pattern "*.gz" --parallel 8 --dry-run
`

func main() {
	var (
		source      string
		destination string
		days        int
		pattern     string
		excludeList cloudutil.MultiFlag
		parallel    int
		project     string
		credentials string
		retries     int
		copyMode    bool
		noVerify    bool
		dryRun      bool
		help        bool
	)

	fs := flag.NewFlagSet("global-gcp-backup", flag.ExitOnError)
	fs.StringVar(&source, "source", "", "")
	fs.StringVar(&destination, "destination", "", "")
	fs.IntVar(&days, "days", 0, "")
	fs.StringVar(&pattern, "pattern", "*", "")
	fs.Var(&excludeList, "exclude", "")
	fs.IntVar(&parallel, "parallel", 4, "")
	fs.StringVar(&project, "project", "", "")
	fs.StringVar(&credentials, "credentials", "", "")
	fs.IntVar(&retries, "retries", 3, "")
	fs.BoolVar(&copyMode, "copy", false, "")
	fs.BoolVar(&noVerify, "no-verify", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&help, "h", false, "")
	fs.BoolVar(&help, "help", false, "")
	fs.Usage = func() { fmt.Print(usage) }
	fs.Parse(os.Args[1:])

	if help || source == "" || destination == "" || days == 0 {
		fmt.Print(usage)
		os.Exit(0)
	}

	bucket, prefix, err := gcpclient.ParseGCSURL(destination)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	cutoff := time.Now().Truncate(24 * time.Hour).AddDate(0, 0, -days)

	// Collect matching files (local FS only — no credentials needed)
	var candidates []string
	err = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !cloudutil.GlobMatch(name, pattern) {
			return nil
		}
		for _, ex := range excludeList {
			if cloudutil.GlobMatch(name, ex) || cloudutil.GlobMatch(path, ex) {
				return nil
			}
		}
		t, ok := cloudutil.ExtractDate(name)
		if !ok || !t.Before(cutoff) {
			return nil
		}
		candidates = append(candidates, path)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR walking source:", err)
		os.Exit(1)
	}

	if len(candidates) == 0 {
		fmt.Printf("No files matching criteria found in %s\n", source)
		os.Exit(0)
	}

	action := "move"
	if copyMode {
		action = "copy"
	}
	fmt.Printf("Found %d file(s) to %s (cutoff: %s)\n", len(candidates), action, cutoff.Format("2006-01-02"))

	// Dry-run: print and exit without touching credentials
	if dryRun {
		for _, path := range candidates {
			blob := cloudutil.BuildObjectPath(path, prefix, hostname)
			fmt.Printf("[DRY-RUN] Would %s: %s -> gs://%s/%s\n", action, path, bucket, blob)
		}
		fmt.Printf("\nTotal: %d file(s)\n", len(candidates))
		return
	}

	ctx := context.Background()
	client, err := gcpclient.NewClient(ctx, gcpclient.Config{Project: project, Credentials: credentials})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: create GCS client:", err)
		os.Exit(1)
	}
	defer client.Close()

	var (
		ok   int64
		fail int64
		wg   sync.WaitGroup
		sem  = make(chan struct{}, parallel)
	)

	for _, path := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(localPath string) {
			defer wg.Done()
			defer func() { <-sem }()

			blob := cloudutil.BuildObjectPath(localPath, prefix, hostname)
			uploadErr := client.Upload(ctx, localPath, bucket, blob, gcpclient.UploadOptions{
				Verify:  !noVerify,
				Retries: retries,
			})
			if uploadErr != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", localPath, uploadErr)
				atomic.AddInt64(&fail, 1)
				return
			}

			verb := "Moved"
			if copyMode {
				verb = "Copied"
			} else {
				os.Remove(localPath)
			}
			fmt.Printf("%s: %s -> gs://%s/%s\n", verb, localPath, bucket, blob)
			atomic.AddInt64(&ok, 1)
		}(path)
	}
	wg.Wait()

	fmt.Printf("\nDone: %d succeeded, %d failed\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}
