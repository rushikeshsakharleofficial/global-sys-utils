# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`global-sys-utils` is a system utilities package. The primary tool is `global-logrotate` — a Go-based log rotation utility with AES-256 encryption support. Legacy bash scripts for AWS S3 and GCP cloud backup/restore also live here.

Current version: **2.1.15** (set in `Makefile`)

## Build Commands

```bash
# Build for native architecture → build/global-logrotate
make build

# Build for amd64 and arm64
make build-all

# Install locally (/usr/local/bin/, /etc/global-sys-utils/, completions)
make install

# Clean build artifacts
make clean
```

## Packaging

```bash
# RPM for native arch
make rpm

# RPM for specific arch
make rpm GOARCH=amd64
make rpm GOARCH=arm64

# DEB for native arch
make deb

# DEB for specific arch
make deb GOARCH=amd64

# All packages, all arches (amd64 + arm64, RPM + DEB)
make packages-all
```

Output locations:
- Binaries: `build/global-logrotate-{amd64,arm64}`
- RPMs: `build/rpm/{x86_64,aarch64}/RPMS/`
- DEBs: `build/deb/*.deb`

## Architecture

### Source

All Go code is in a single file: `cmd/global-logrotate/main.go` (~1400 lines).

**Execution flow:**
1. `parseFlags()` — merges config files + CLI flags (CLI takes precedence)
2. `loadConfigFiles()` — reads `/etc/global-sys-utils/global.conf` then drop-ins from `global.conf.d/`
3. `initLogger()` — structured logging with levels (error/info/debug)
4. Special modes: `--pass-gen`, `--pass-reset`, `--read` handled before rotation
5. Encryption validation if enabled
6. `findLogFiles()` — glob matching with exclude patterns
7. `rotateSequential()` or `rotateParallel()` depending on `--jobs`

**Encryption system:**
- AES-256-GCM with magic bytes `GLRE` at start of encrypted files
- PBKDF2 key derivation (100k iterations, SHA-256)
- Password stored as bcrypt hash in `~/.global-sys-utils/config/credentials.ini`
- Encrypted files are optionally gzip-compressed (`.gz` suffix)

**Config hierarchy:**
1. `/etc/global-sys-utils/global.conf` (base config)
2. `/etc/global-sys-utils/global.conf.d/*.conf` (drop-ins, alphabetical order)
3. CLI flags (highest priority)

### Cloud scripts (root level, Python)

All four cloud scripts are Python 3 using native SDKs (no CLI dependency):

| Script | SDK | Purpose |
|---|---|---|
| `global-aws-backup` | `boto3` | Move/copy aged log files to S3 |
| `global-aws-restore` | `boto3` | Download files from S3 |
| `global-gcp-backup` | `google-cloud-storage` | Move/copy aged log files to GCS |
| `global-gcp-restore` | `google-cloud-storage` | Download files from GCS |

**Shared features across all four:** `--dry-run`, `--parallel N`, `--pattern`, `--exclude` (repeatable), `--retries`, `--flatten` (restore only).

**Backup-specific:** `--copy` (preserve source), `--no-verify` (skip MD5 check after upload).

**Auth:**
- AWS: `--profile` / `--region`, or standard boto3 credential chain (env vars, `~/.aws/`)
- GCP: `--credentials /path/to/sa-key.json` or ADC (`GOOGLE_APPLICATION_CREDENTIALS`)

**S3 key / GCS blob path structure:** `{prefix}/{hostname}/{relative_dir}/{filename}`

**Legacy bash scripts (root level)**

- `global-logrotate` — original bash implementation (superseded by Go binary)

### Packaging templates

- `packaging/rpm/global-logrotate.spec` — RPM spec (version/release injected by Makefile via `--define`)
- `packaging/deb/control` — DEB control file (`{{VERSION}}` and `{{ARCH}}` substituted by Makefile `sed`)
- The root `global-sys-utils.spec` is **deprecated** (bash-era, v1.0.x)

## Go Module

```
module github.com/rushikeshsakharleofficial/global-logrotate
go 1.24.0
```

Dependencies: `golang.org/x/crypto` (PBKDF2), `golang.org/x/term` (password input)

## No Tests

There are currently no `*_test.go` files in this repository.

## Version Bumping

Version is defined only in the `Makefile` (`VERSION := 2.1.15`). The RPM spec and DEB control use `%{_version}` / `{{VERSION}}` placeholders — the Makefile injects the actual value at build time.
