package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rushikeshsakharleofficial/global-logrotate/pkg/awsclient"
)

const usage = `Usage: global-aws-restore --source <s3://bucket[/prefix]> --destination <path> [OPTIONS]

Restore log files from AWS S3.

Options:
  --source       <s3://…>        S3 source URL (bucket and optional prefix)
  --destination  <path>          Local directory to restore into
  --pattern      <glob>          Filename glob to include (default: *)
  --exclude      <glob>          Filename glob to exclude; repeatable
  --parallel     <N>             Concurrent downloads (default: 4)
  --profile      <name>          AWS profile
  --region       <region>        AWS region
  --retries      <N>             Download retry count (default: 3)
  --flatten                      Put all files directly in destination (no subdirs)
  --dry-run                      Print actions without downloading anything
  -h, --help                     Show this help

Examples:
  global-aws-restore --source s3://my-bucket/logs --destination /var/log/restore
  global-aws-restore --source s3://my-bucket/logs/myhost --destination /tmp/restore \
      --pattern "*.gz" --parallel 8 --flatten --dry-run
`

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

func localPath(key, prefix, dest string, flatten bool) string {
	if flatten {
		return filepath.Join(dest, filepath.Base(key))
	}
	relative := strings.TrimLeft(strings.TrimPrefix(key, prefix), "/")
	return filepath.Join(dest, relative)
}

func main() {
	var (
		source      string
		destination string
		pattern     string
		excludeList multiFlag
		parallel    int
		profile     string
		region      string
		retries     int
		flatten     bool
		dryRun      bool
		help        bool
	)

	fs := flag.NewFlagSet("global-aws-restore", flag.ExitOnError)
	fs.StringVar(&source, "source", "", "")
	fs.StringVar(&destination, "destination", "", "")
	fs.StringVar(&pattern, "pattern", "*", "")
	fs.Var(&excludeList, "exclude", "")
	fs.IntVar(&parallel, "parallel", 4, "")
	fs.StringVar(&profile, "profile", "", "")
	fs.StringVar(&region, "region", "", "")
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

	bucket, prefix, err := parseS3URL(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := awsclient.NewClient(ctx, awsclient.Config{Profile: profile, Region: region})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: create AWS client:", err)
		os.Exit(1)
	}

	listPrefix := prefix
	if listPrefix != "" {
		listPrefix += "/"
	}
	fmt.Printf("Listing objects under s3://%s/%s ...\n", bucket, prefix)
	objects, err := client.ListObjects(ctx, bucket, listPrefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: list objects:", err)
		os.Exit(1)
	}

	// Filter by pattern and exclude
	var candidates []awsclient.ObjectInfo
	for _, obj := range objects {
		name := filepath.Base(obj.Key)
		if !globMatch(name, pattern) {
			continue
		}
		excluded := false
		for _, ex := range excludeList {
			if globMatch(name, ex) || globMatch(obj.Key, ex) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		candidates = append(candidates, obj)
	}

	if len(candidates) == 0 {
		fmt.Println("No objects found matching criteria.")
		os.Exit(0)
	}
	fmt.Printf("Found %d object(s) to restore\n", len(candidates))

	if dryRun {
		for _, obj := range candidates {
			dest := localPath(obj.Key, prefix, destination, flatten)
			fmt.Printf("[DRY-RUN] Would download: s3://%s/%s -> %s\n", bucket, obj.Key, dest)
		}
		fmt.Printf("\nTotal: %d object(s)\n", len(candidates))
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

	for _, obj := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(o awsclient.ObjectInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			dest := localPath(o.Key, prefix, destination, flatten)

			if _, err := os.Stat(dest); err == nil {
				fmt.Printf("Skipping (exists): %s\n", dest)
				atomic.AddInt64(&ok, 1)
				return
			}

			dlErr := client.Download(ctx, bucket, o.Key, dest, awsclient.DownloadOptions{Retries: retries})
			if dlErr != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", o.Key, dlErr)
				atomic.AddInt64(&fail, 1)
				return
			}
			fmt.Printf("Restored: s3://%s/%s -> %s\n", bucket, o.Key, dest)
			atomic.AddInt64(&ok, 1)
		}(obj)
	}
	wg.Wait()

	fmt.Printf("\nDone: %d succeeded, %d failed\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

type multiFlag []string

func (m *multiFlag) String() string        { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error    { *m = append(*m, v); return nil }
