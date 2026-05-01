package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rushikeshsakharleofficial/global-logrotate/internal/policy"
)

var (
	policyPath = flag.String("policy", "/etc/global-sys-utils/policy.conf", "Path to policy file")
	dryRun     = flag.Bool("dry-run", false, "Show actions without executing")
)

func main() {
	flag.Parse()

	p, err := policy.Load(*policyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load policy: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
	for _, r := range p.Rules {
		files, _ := filepath.Glob(filepath.Join(r.Path, r.Pattern))
		for _, f := range files {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if !policy.ShouldRotate(info, r, now) {
				continue
			}
			rotate(f, r)
		}
	}
}

func rotate(path string, r policy.Rule) {
	dir := filepath.Join(r.Path, r.OldDir)
	_ = os.MkdirAll(dir, 0755)

ts := time.Now().Format("20060102T150405")
	base := filepath.Base(path)
	target := filepath.Join(dir, base+"."+ts)

	if *dryRun {
		fmt.Printf("[DRY] rotate %s -> %s (%s)\n", path, target, r.Mode)
		return
	}

	switch r.Mode {
	case policy.ModeRenameCreate:
		_ = os.Rename(path, target)
		createEmpty(path, r)
	case policy.ModeCopyTruncate:
		copyFile(path, target)
		_ = os.Truncate(path, 0)
	case policy.ModeSignalReopen:
		_ = os.Rename(path, target)
		if r.SIGHUPPid > 0 {
			_ = syscall.Kill(r.SIGHUPPid, syscall.SIGHUP)
		}
		createEmpty(path, r)
	}

	if r.Compress {
		gz := target + ".gz"
		compress(target, gz)
		_ = os.Remove(target)
	}
}

func createEmpty(path string, r policy.Rule) {
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, r.CreateMode)
	f.Close()
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer out.Close()
	io.Copy(out, in)
}

func compress(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	io.Copy(gz, in)
}
