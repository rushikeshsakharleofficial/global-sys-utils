package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/awsclient"
)

const usage = `Usage: global-aws-backup --source <path> --destination <s3://bucket[/prefix]> --days <N> [OPTIONS]

Backup log files to AWS S3.

Options:
  --source       <path>          Source directory to scan
  --destination  <s3://…>        S3 destination URL
  --days         <N>             Process files whose name contains a date older than N days
  --pattern      <glob>          Filename glob to include (default: *)
  --exclude      <glob>          Filename glob to exclude; repeatable
  --parallel     <N>             Concurrent uploads (default: 4)
  --profile      <name>          AWS profile
  --region       <region>        AWS region
  --retries      <N>             Upload retry count (default: 3)
  --copy                         Copy files instead of moving (preserve source)
  --no-verify                    Skip MD5 checksum verification after upload
  --dry-run                      Print actions without making any changes
  -h, --help                     Show this help

Examples:
  global-aws-backup --source /var/log/apps --destination s3://my-bucket/logs --days 30
  global-aws-backup --source /var/log/apps --destination s3://my-bucket/logs --days 30 \
      --pattern "*.gz" --parallel 8 --dry-run
`

var dateRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})|(\d{8})`)

func extractDate(name string) (time.Time, bool) {
	m := dateRe.FindString(name)
	if m == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"20060102", "2006-01-02"} {
		if t, err := time.Parse(layout, m); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseS3URL(raw string) (bucket, prefix string, err error) {
	if !strings.HasPrefix(raw, "s3://") {
		return "", "", fmt.Errorf("invalid S3 URL %q: must start with s3://", raw)
	}
	rest := strings.TrimPrefix(raw, "s3://")
	parts := strings.SplitN(rest, "/", 2)
	bucket = parts[0]
	if len(parts) == 2 {
		prefix = strings.TrimRight(parts[1], "/")
	}
	return
}

func globMatch(name, pattern string) bool {
	matched, _ := filepath.Match(pattern, name)
	return matched
}

func buildKey(path, sourceRoot, prefix, hostname string) string {
	relDir := strings.TrimLeft(filepath.Dir(path), "/")
	parts := []string{}
	for _, p := range []string{prefix, hostname, relDir, filepath.Base(path)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

func main() {
	var (
		source      string
		destination string
		days        int
		pattern     string
		excludeList multiFlag
		parallel    int
		profile     string
		region      string
		retries     int
		copyMode    bool
		noVerify    bool
		dryRun      bool
		help        bool
	)

	fs := flag.NewFlagSet("global-aws-backup", flag.ExitOnError)
	fs.StringVar(&source, "source", "", "")
	fs.StringVar(&destination, "destination", "", "")
	fs.IntVar(&days, "days", 0, "")
	fs.StringVar(&pattern, "pattern", "*", "")
	fs.Var(&excludeList, "exclude", "")
	fs.IntVar(&parallel, "parallel", 4, "")
	fs.StringVar(&profile, "profile", "", "")
	fs.StringVar(&region, "region", "", "")
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

	bucket, prefix, err := parseS3URL(destination)
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
		if !globMatch(name, pattern) {
			return nil
		}
		for _, ex := range excludeList {
			if globMatch(name, ex) || globMatch(path, ex) {
				return nil
			}
		}
		t, ok := extractDate(name)
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
			key := buildKey(path, source, prefix, hostname)
			fmt.Printf("[DRY-RUN] Would %s: %s -> s3://%s/%s\n", action, path, bucket, key)
		}
		fmt.Printf("\nTotal: %d file(s)\n", len(candidates))
		return
	}

	ctx := context.Background()
	client, err := awsclient.NewClient(ctx, awsclient.Config{Profile: profile, Region: region})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: create AWS client:", err)
		os.Exit(1)
	}

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

			key := buildKey(localPath, source, prefix, hostname)
			uploadErr := client.Upload(ctx, localPath, bucket, key, awsclient.UploadOptions{
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
			fmt.Printf("%s: %s -> s3://%s/%s\n", verb, localPath, bucket, key)
			atomic.AddInt64(&ok, 1)
		}(path)
	}
	wg.Wait()

	fmt.Printf("\nDone: %d succeeded, %d failed\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

// multiFlag allows --exclude to be specified multiple times.
type multiFlag []string

func (m *multiFlag) String() string  { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
