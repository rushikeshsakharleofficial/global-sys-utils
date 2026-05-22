<div align="center">

# global-sys-utils

**High-performance log rotation with cloud backup, daemon scheduling, and disk-pressure protection.**

[![Build](https://github.com/rushikeshsakharleofficial/global-sys-utils/actions/workflows/packages.yml/badge.svg)](https://github.com/rushikeshsakharleofficial/global-sys-utils/actions/workflows/packages.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

---

`global-sys-utils` is a suite of Linux system utilities for log management:

- **`global-logrotate`** — a Go binary that compresses, optionally encrypts (AES-256-GCM), and truncates log files. Runs on demand, on a cron schedule, or as a long-running daemon that monitors disk usage and triggers emergency rotation when space runs low.
- **`global-aws-backup` / `global-aws-restore`** — Python scripts for moving aged log archives to and from AWS S3.
- **`global-gcp-backup` / `global-gcp-restore`** — Python scripts for moving aged log archives to and from Google Cloud Storage.

All tools are packaged as `.deb` and `.rpm` for Debian/Ubuntu and RHEL/CentOS/Fedora.

---

## Table of Contents

1. [Features](#features)
2. [Requirements](#requirements)
3. [Installation](#installation)
4. [Quick Start](#quick-start)
5. [global-logrotate](#global-logrotate)
6. [Cloud Backup Tools](#cloud-backup-tools)
7. [Daemon Mode](#daemon-mode)
8. [Configuration Reference](#configuration-reference)
9. [Build from Source](#build-from-source)
10. [Testing](#testing)
11. [Project Structure](#project-structure)
12. [Security](#security)
13. [License](#license)

---

## Features

- **Parallel rotation** — rotate N files concurrently with `--parallel`
- **AES-256-GCM encryption** — per-user password, stored as SHA-256 hash; credentials auto-loaded at runtime
- **Atomic writes** — archive written to `.tmp` then renamed before the source is truncated; crash-safe
- **Daemon mode** — `--daemon` / `--daemon-once` with cron or interval scheduling (`0 2 * * *`, `6h`, `@daily`)
- **Real-time disk monitoring** — configurable threshold triggers immediate rotation when disk fills; per-file guard refuses to write an archive when free space is critically low
- **Cloud integration in daemon** — automatically calls `global-aws-backup` or `global-gcp-backup` after scheduled rotation or on disk-pressure events
- **Adaptive upload throttle** — concurrent uploads self-limit based on live CPU/RAM readings (psutil); prevents OOM on small VMs
- **Per-application conf.d jobs** — each `/etc/global-sys-utils/global.conf.d/*.conf` file is an independent rotation job with its own schedule, directory, and cloud target
- **systemd units** — long-running service and oneshot + timer variants included in packages
- **DEB and RPM packages** — Python dependencies installed via `pip` during package post-install

---

## Requirements

| Component | Minimum |
|---|---|
| OS | Linux (x86\_64 or arm64) |
| Go | 1.25+ (build only) |
| Python | 3.8+ (cloud tools) |
| pip | 23+ recommended |
| systemd | 232+ (optional, for daemon units) |

Python runtime dependencies (installed automatically by package post-install):

```text
boto3>=1.34.0
google-cloud-storage>=2.16.0
google-auth>=2.28.0
psutil>=5.9.0
```

---

## Installation

### From pre-built packages (recommended)

Pre-built packages for every release are in the [`installers/`](installers/) directory.

**Debian / Ubuntu:**

```bash
sudo dpkg -i installers/v2.1.15/global-logrotate_2.1.15-1_amd64.deb
```

**RHEL / CentOS / Fedora (x86\_64):**

```bash
sudo rpm -ivh installers/v2.1.15/global-logrotate-2.1.15-1.x86_64.rpm
```

**ARM64 packages** (`aarch64` / `arm64`) are in the same directory.

Both installers place binaries in `/usr/bin/`, config files in `/etc/global-sys-utils/`, systemd units in `/usr/lib/systemd/system/`, and run `pip3 install` for the Python dependencies automatically.

### Manual Python dependency install

If post-install pip fails or you install from source:

```bash
pip3 install -r requirements.txt
```

---

## Quick Start

### One-shot log rotation

```bash
# Rotate all *.log files in /var/log/myapp, date-stamp them, and compress
global-logrotate -D -p /var/log/myapp

# Dry-run to preview what would be rotated
global-logrotate -D -p /var/log/myapp -n
```

### Encrypt rotated logs

```bash
# First-time: generate and store a password
global-logrotate --pass-gen

# Rotate with encryption
global-logrotate --encrypt -D -p /var/log/myapp

# Read an encrypted archive
global-logrotate --read /var/log/myapp/old_logs/20240115/app.log.20240115.gz.enc
```

### Run as a systemd daemon

```bash
# Add a schedule to the config
echo "SCHEDULE = 0 2 * * *" | sudo tee -a /etc/global-sys-utils/global.conf

# Enable and start
sudo systemctl enable --now global-logrotate
sudo journalctl -u global-logrotate -f
```

---

## global-logrotate

### Synopsis

```text
global-logrotate [OPTIONS]
```

### Options

| Flag | Default | Description |
|---|---|---|
| `-D` | — | Date-only suffix (`YYYYMMDD`) |
| `-H` | — | Full timestamp suffix (`YYYYMMDDTHH:MM:SS`) |
| `--pattern <glob>` | `*.log` | File glob to rotate |
| `-p <path>` | `/var/log/apps` | Source log directory |
| `-o <path>` | `<logdir>/old_logs` | Archive output directory |
| `--exclude-from <file>` | — | File of glob patterns to skip |
| `--parallel <N>` | `4` | Concurrent rotations |
| `-n` | — | Dry-run: show actions, make no changes |
| `--encrypt` | — | AES-256-GCM encrypt each archive |
| `--read <file>` | — | Decompress (and decrypt) a rotated file to stdout |
| `--pass-gen` | — | First-time password setup |
| `--pass-reset` | — | Change encryption password |
| `--daemon` | — | Run scheduling loop (reads `SCHEDULE` from config) |
| `--daemon-once` | — | Run all jobs once then exit (for systemd timers) |
| `--log-file <path>` | `/var/log/global-sys-utils/global-logrotate.log` | Log file path |
| `--log-level <level>` | `info` | `error` \| `info` \| `debug` |
| `--version` | — | Print version and exit |

### Archive layout

```
<old_logs_dir>/
└── YYYYMMDD/
    ├── app.log.YYYYMMDD.gz          # plain compressed
    └── error.log.YYYYMMDD.gz.enc    # compressed + encrypted
```

---

## Cloud Backup Tools

All four tools share a consistent interface. Use `--dry-run` to preview before acting.

### global-aws-backup

Move aged log archives from a local directory to an S3 bucket.

```bash
global-aws-backup \
  --source /var/log/apps/old_logs \
  --destination s3://my-bucket/logs \
  --days 7 \
  --parallel 4 \
  --region us-east-1
```

| Flag | Default | Description |
|---|---|---|
| `--source <path>` | required | Local directory to scan |
| `--destination <url>` | required | `s3://bucket[/prefix]` |
| `--days <N>` | required | Only process files dated older than N days (must be ≥ 1) |
| `--pattern <glob>` | `*` | Filename glob to include |
| `--exclude <glob>` | — | Glob to skip (repeatable) |
| `--parallel <N>` | `4` | Max concurrent uploads; adaptive throttle may use fewer |
| `--timeout <sec>` | `300` | Per-operation timeout |
| `--profile` | — | AWS profile name |
| `--region` | — | AWS region |
| `--retries <N>` | `3` | Retry count with exponential backoff |
| `--copy` | — | Copy instead of move (preserve source) |
| `--no-verify` | — | Skip MD5 checksum after upload |
| `--dry-run` | — | Print actions without uploading |
| `--verbose` | — | Enable debug logging |

### global-aws-restore

Download archives from S3 back to local storage.

```bash
global-aws-restore \
  --source s3://my-bucket/logs \
  --destination /var/log/restore \
  --pattern "*.gz" \
  --flatten
```

### global-gcp-backup

```bash
global-gcp-backup \
  --source /var/log/apps/old_logs \
  --destination gs://my-bucket/logs \
  --days 7 \
  --project my-gcp-project
```

### global-gcp-restore

```bash
global-gcp-restore \
  --source gs://my-bucket/logs \
  --destination /var/log/restore \
  --flatten
```

All restore tools accept `--flatten` to place all downloaded files directly in the destination directory (no subdirectory tree).

---

## Daemon Mode

`--daemon` starts a scheduling loop that persists indefinitely, monitors disk usage, and runs jobs from the config files.

### Enable daemon via systemd

```bash
# Option A — long-running daemon (disk monitoring + scheduling)
sudo systemctl enable --now global-logrotate

# Option B — systemd timer (no disk monitoring)
sudo systemctl enable --now global-logrotate-once.timer
```

### Override the timer schedule without editing the unit

```bash
sudo mkdir -p /etc/systemd/system/global-logrotate-once.timer.d/
cat <<EOF | sudo tee /etc/systemd/system/global-logrotate-once.timer.d/schedule.conf
[Timer]
OnCalendar=
OnCalendar=*-*-* 04:30:00
EOF
sudo systemctl daemon-reload
```

### Schedule formats

| Format | Example | Meaning |
|---|---|---|
| Cron (5-field) | `0 2 * * *` | Daily at 02:00 |
| Interval | `6h` | Every 6 hours |
| Interval | `30m` | Every 30 minutes |
| Interval | `7d` | Weekly |
| Alias | `@daily` | Same as `0 0 * * *` |
| Alias | `@hourly` | Same as `0 * * * *` |
| Alias | `@weekly` | Same as `0 0 * * 0` |
| Alias | `@monthly` | Same as `0 0 1 * *` |

### Disk pressure

| Key | Default | Behaviour |
|---|---|---|
| `DISK_CRITICAL_PERCENT` | `90` | Trigger emergency rotation immediately when disk reaches this % |
| `DISK_MIN_FREE_MB` | `200` | Refuse to write an archive if free space would drop below this |
| `DISK_CHECK_INTERVAL` | `60` | Seconds between disk checks |

When disk reaches `DISK_CRITICAL_PERCENT`, the daemon rotates affected jobs immediately, then optionally ships logs to the cloud if `CLOUD_BACKUP_ON_PANIC = true` is set for that job.

---

## Configuration Reference

Main config: `/etc/global-sys-utils/global.conf`
Drop-in jobs: `/etc/global-sys-utils/global.conf.d/*.conf`

In **daemon mode**, each `.conf` file in `global.conf.d/` is treated as an independent rotation job that inherits defaults from `global.conf`. A file without `SCHEDULE` is ignored by the daemon but still effective for on-demand runs.

### Rotation

| Key | Default | Description |
|---|---|---|
| `LOG_DIR` | `/var/log/apps` | Directory to scan |
| `PATTERN` | `*.log` | Glob pattern |
| `OLD_LOGS_DIR` | `<logdir>/old_logs` | Archive output root |
| `EXCLUDE_FILE` | — | Path to file with one exclude glob per line |
| `PARALLEL_JOBS` | `4` | Concurrent rotations |
| `DATE_FORMAT` | `date` | `date` (YYYYMMDD) or `full` (YYYYMMDDTHH:MM:SS) |
| `DRY_RUN` | `false` | Log actions without changes |
| `ENCRYPT` | `false` | AES-256-GCM encryption |

### Scheduling (daemon)

| Key | Default | Description |
|---|---|---|
| `SCHEDULE` | — | Cron expression, interval, or `@alias` |
| `PID_FILE` | `/run/global-logrotate.pid` | PID file path |

### Disk safety

| Key | Default | Description |
|---|---|---|
| `DISK_CRITICAL_PERCENT` | `90` | Emergency rotation threshold |
| `DISK_MIN_FREE_MB` | `200` | Minimum free MB to write archive |
| `DISK_CHECK_INTERVAL` | `60` | Disk check interval (seconds) |

### Cloud backup (daemon)

| Key | Default | Description |
|---|---|---|
| `CLOUD_PROVIDER` | — | `aws` or `gcp` |
| `CLOUD_SOURCE` | `<old_logs_dir>` | Local source for cloud uploads |
| `CLOUD_DESTINATION` | — | `s3://…` or `gs://…` |
| `CLOUD_DAYS` | `1` | Upload files older than N days |
| `CLOUD_PARALLEL` | `4` | Concurrent uploads |
| `CLOUD_TIMEOUT` | `300` | Per-operation timeout (seconds) |
| `CLOUD_AWS_PROFILE` | — | AWS named profile |
| `CLOUD_AWS_REGION` | — | AWS region |
| `CLOUD_GCP_PROJECT` | — | GCP project ID |
| `CLOUD_GCP_CREDENTIALS` | — | Path to GCP service account JSON |
| `CLOUD_BACKUP_ON_SCHEDULE` | `false` | Run cloud backup after each scheduled rotation |
| `CLOUD_BACKUP_ON_PANIC` | `false` | Run cloud backup when disk hits critical threshold |

### Logging

| Key | Default | Description |
|---|---|---|
| `LOG_FILE` | `/var/log/global-sys-utils/global-logrotate.log` | Log output path |
| `LOG_LEVEL` | `info` | `error` \| `info` \| `debug` |

### Per-app conf.d example

```ini
# /etc/global-sys-utils/global.conf.d/nginx.conf
LOG_DIR      = /var/log/nginx
PATTERN      = *.log
SCHEDULE     = 0 2 * * *
PARALLEL_JOBS = 2

DISK_CRITICAL_PERCENT = 85
DISK_MIN_FREE_MB      = 500

CLOUD_PROVIDER          = aws
CLOUD_DESTINATION       = s3://my-bucket/nginx-logs
CLOUD_AWS_REGION        = us-east-1
CLOUD_BACKUP_ON_SCHEDULE = true
CLOUD_BACKUP_ON_PANIC    = true
```

---

## Build from Source

### Prerequisites

- Go 1.25+
- `rpmbuild` (for RPM packages)
- `dpkg-deb` (for DEB packages)

### Commands

```bash
# Build binary for current architecture
make build

# Build for amd64 and arm64
make build-all

# Build DEB package (native arch)
make deb

# Build RPM package (native arch)
make rpm

# Build all packages for all architectures
make packages-all

# Install to /usr/bin (requires root)
sudo make install

# Remove build artifacts
make clean
```

CI triggers automatically on push to `main` when files under `cmd/`, `packaging/`, `config/`, `completions/`, or `man/` change. Built packages are committed to `installers/v<VERSION>/`.

---

## Testing

```bash
make test
```

This runs:

1. **Go unit tests** with the race detector (`go test ./cmd/global-logrotate/ -race`):
   - Schedule parsing: cron fields, `cronNext`, interval strings, shorthands, error cases
   - Compression: gzip roundtrip, empty input, corrupt input
   - Encryption: AES-256-GCM roundtrip, wrong-password rejection, bad magic bytes, nondeterminism
   - Rotation integration: basic, encrypted, dry-run, already-rotated idempotency, disk-guard, parallel (5 files × 3 workers), permission bit stripping
   - Config helpers, `buildConfig` defaults and overrides, disk stats

2. **Python utility tests** (`pytest tests/test_utils.py`):
   - Date extraction, URL parsing, key/path construction, MD5 — no cloud SDK required
   - `AdaptiveThrottle` concurrent cap enforcement
   - Retry logic: N attempts on repeated failure, recovery on second attempt
   - All cloud SDKs fully mocked; no credentials needed

---

## Project Structure

```
global-sys-utils/
├── cmd/
│   ├── global-logrotate/       # Go source for the log rotation binary
│   │   ├── main.go
│   │   └── main_test.go
│   ├── global-aws-backup       # Python: upload aged logs to AWS S3
│   ├── global-aws-restore      # Python: download logs from AWS S3
│   ├── global-gcp-backup       # Python: upload aged logs to GCP GCS
│   └── global-gcp-restore      # Python: download logs from GCP GCS
├── completions/                # Bash and zsh completions for global-logrotate
├── config/
│   ├── global.conf             # Main configuration (installed to /etc/global-sys-utils/)
│   └── global.conf.d/
│       └── example.conf        # Annotated per-app job example
├── installers/                 # Pre-built .deb and .rpm packages per release
├── man/
│   └── global-logrotate.1      # Man page
├── packaging/
│   ├── deb/                    # Debian packaging (control, postinst, prerm, conffiles)
│   ├── rpm/                    # RPM spec
│   └── systemd/                # systemd service and timer units
├── tests/
│   └── test_utils.py           # Python utility tests
├── go.mod
├── requirements.txt            # Python runtime dependencies
├── Makefile
└── LICENSE
```

---

## Contributing

1. Fork the repository and create a feature branch: `git checkout -b feature/my-change`
2. Make changes, run `make test` — all tests must pass with the race detector clean
3. For Go changes, run `go vet ./...` before committing
4. Open a pull request against `main` with a clear description of what changed and why
5. Package builds are triggered automatically by CI on merge to `main`

Bug reports and feature requests: open a [GitHub issue](https://github.com/rushikeshsakharleofficial/global-sys-utils/issues).

---

## Security

### Encryption

Log archives can be encrypted with AES-256-GCM. The plaintext password is never stored — only its SHA-256 hash is written to `/etc/global-sys-utils/global.conf.d/encryption.conf`. The password itself is saved to `~/.global-sys-utils/config/credentials.ini` (mode `0600`).

```bash
global-logrotate --pass-gen     # initial setup
global-logrotate --pass-reset   # change password
```

Password sources are checked in this order: credentials file → `LOGROTATE_PASSWORD` environment variable → interactive prompt.

### Reporting vulnerabilities

Open a [GitHub Security Advisory](https://github.com/rushikeshsakharleofficial/global-sys-utils/security/advisories/new) for any security issue. Do not file public issues for vulnerabilities.

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
