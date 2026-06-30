<div align="center">

# global-sys-utils

**Automated log rotation for Linux with AWS S3 and Google Cloud Storage backup, AES-256-GCM encryption, systemd daemon scheduling, and real-time disk-pressure protection.**

[![Build](https://img.shields.io/github/actions/workflow/status/rushikeshsakharleofficial/global-sys-utils/packages.yml?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/global-sys-utils/actions)
[![License](https://img.shields.io/github/license/rushikeshsakharleofficial/global-sys-utils?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/global-sys-utils/blob/main/LICENSE)
[![Version](https://img.shields.io/github/v/release/rushikeshsakharleofficial/global-sys-utils?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/global-sys-utils/releases)
[![Stars](https://img.shields.io/github/stars/rushikeshsakharleofficial/global-sys-utils?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/global-sys-utils/stargazers)

</div>

## What is this?

`global-sys-utils` is an open-source Linux log management suite that combines fast parallel log rotation (Go), encrypted archiving, cloud offload to AWS S3 and Google Cloud Storage (Python), and a self-scheduling daemon with live disk monitoring. All driven by a simple INI configuration file and packaged as `.deb` / `.rpm` for production Linux systems.

---

## Table of Contents

1. [Why global-sys-utils?](#why-global-sys-utils)
2. [Features](#features)
3. [Requirements](#requirements)
4. [Installation](#installation)
5. [Quick Start](#quick-start)
6. [global-logrotate](#global-logrotate)
7. [Cloud Backup Tools](#cloud-backup-tools)
8. [Daemon Mode](#daemon-mode)
9. [Configuration Reference](#configuration-reference)
10. [Build from Source](#build-from-source)
11. [Testing](#testing)
12. [Maintenance](#maintenance)
13. [Project Structure](#project-structure)
14. [Documentation](#documentation)
15. [Contributing](#contributing)
16. [Security](#security)
17. [License](#license)

---

## Why global-sys-utils?

The standard `logrotate` daemon lacks cloud offload, built-in encryption, and real-time disk-pressure response. `global-sys-utils` fills that gap:

| Capability | `logrotate` | `global-sys-utils` |
|---|:---:|:---:|
| Parallel rotation | ✗ | ✓ |
| AES-256-GCM encryption | ✗ | ✓ |
| AWS S3 backup | ✗ | ✓ |
| Google Cloud Storage backup | ✗ | ✓ |
| Emergency rotation on disk pressure | ✗ | ✓ |
| Per-file disk space guard | ✗ | ✓ |
| Daemon with live scheduling | limited | ✓ |
| systemd service + timer units | limited | ✓ |
| Cron expressions + intervals | via cron | built-in |
| DEB + RPM packages | ✓ | ✓ |

---

## Features

- **Parallel rotation** — rotate N log files concurrently; sorted by size for optimal throughput
- **AES-256-GCM encryption** — per-user password stored only as SHA-256 hash; credentials auto-loaded at runtime
- **Atomic writes** — compressed archive written to `.tmp` then renamed; crash during rotation leaves source file intact
- **Daemon mode** — `--daemon` / `--daemon-once` with cron expressions (`0 2 * * *`), intervals (`6h`, `30m`), or aliases (`@daily`, `@hourly`)
- **Real-time disk monitoring** — configurable threshold triggers immediate emergency rotation when disk fills
- **Per-file disk guard** — refuses to write an archive when free space would drop below `DISK_MIN_FREE_MB`; source preserved
- **Cloud integration** — daemon calls `global-aws-backup` or `global-gcp-backup` after scheduled rotation or on disk-pressure events
- **Adaptive upload throttle** — concurrent cloud uploads self-limit based on live CPU / RAM readings; prevents OOM on small VMs
- **conf.d job system** — each `/etc/global-sys-utils/global.conf.d/*.conf` is an independent rotation + cloud job
- **systemd units** — long-running service and oneshot+timer variants bundled in packages
- **DEB + RPM packaging** — Python dependencies installed via `pip3` during package post-install with PEP 668 compatibility

---

## Requirements

| Component | Minimum |
|---|---|
| OS | Linux (x86\_64 or arm64) |
| Go | 1.25+ (build only) |
| Python | 3.8+ (cloud tools) |
| pip | 23+ recommended |
| systemd | 232+ (optional, for daemon units) |

**Python runtime dependencies** (installed automatically by package post-install):

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

**Debian / Ubuntu (amd64):**

```bash
sudo dpkg -i installers/v2.2.0/global-logrotate_2.2.0-1_amd64.deb
```

**RHEL / CentOS / Fedora (x86\_64):**

```bash
sudo rpm -ivh installers/v2.2.0/global-logrotate-2.2.0-1.x86_64.rpm
```

**ARM64 packages** (`aarch64` / `arm64`) are in the same directory.

Both installers:
- Place binaries in `/usr/bin/`
- Install config files to `/etc/global-sys-utils/`
- Install systemd units to `/usr/lib/systemd/system/`
- Run `pip3 install` for Python dependencies automatically

### Manual Python dependency install

```bash
pip3 install -r requirements.txt
```

---

## Quick Start

### One-shot log rotation

```bash
# Rotate all *.log files in /var/log/myapp, date-stamp, and gzip-compress
global-logrotate -D -p /var/log/myapp

# Preview without making changes
global-logrotate -D -p /var/log/myapp -n
```

### Encrypt rotated logs

```bash
# First-time setup: generate and store a password
global-logrotate --pass-gen

# Rotate with AES-256-GCM encryption
global-logrotate --encrypt -D -p /var/log/myapp

# Decompress and decrypt an archive to stdout
global-logrotate --read /var/log/myapp/old_logs/20240115/app.log.20240115.gz.enc
```

### Start the daemon (systemd)

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
    ├── app.log.YYYYMMDD.gz          # compressed
    └── error.log.YYYYMMDD.gz.enc    # compressed + encrypted
```

---

## Cloud Backup Tools

All four tools share a consistent CLI interface. Use `--dry-run` to preview before acting.

### global-aws-backup — Upload aged log archives to AWS S3

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
| `--days <N>` | required | Only process files dated older than N days (≥ 1) |
| `--pattern <glob>` | `*` | Filename glob to include |
| `--exclude <glob>` | — | Glob to skip (repeatable) |
| `--parallel <N>` | `4` | Max concurrent uploads |
| `--timeout <sec>` | `300` | Per-operation timeout |
| `--profile` | — | AWS named profile |
| `--region` | — | AWS region |
| `--retries <N>` | `3` | Retry count with exponential backoff + jitter |
| `--copy` | — | Copy instead of move (preserve source) |
| `--no-verify` | — | Skip MD5 checksum after upload |
| `--dry-run` | — | Print actions without uploading |
| `--verbose` | — | Enable debug logging |

### global-aws-restore — Download archives from AWS S3

```bash
global-aws-restore \
  --source s3://my-bucket/logs \
  --destination /var/log/restore \
  --pattern "*.gz" \
  --flatten
```

### global-gcp-backup — Upload aged log archives to Google Cloud Storage

```bash
global-gcp-backup \
  --source /var/log/apps/old_logs \
  --destination gs://my-bucket/logs \
  --days 7 \
  --project my-gcp-project
```

### global-gcp-restore — Download archives from Google Cloud Storage

```bash
global-gcp-restore \
  --source gs://my-bucket/logs \
  --destination /var/log/restore \
  --flatten
```

`--flatten` places all downloaded files directly in the destination directory, skipping subdirectory tree reconstruction.

---

## Daemon Mode

`--daemon` starts a persistent scheduling loop with real-time disk monitoring.

### Enable via systemd

```bash
# Option A — long-running daemon (includes disk monitoring)
sudo systemctl enable --now global-logrotate

# Option B — systemd timer (schedule managed by systemd, no disk monitoring)
sudo systemctl enable --now global-logrotate-once.timer
```

### Override timer schedule without editing the unit

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
| `@daily` | `@daily` | `0 0 * * *` |
| `@hourly` | `@hourly` | `0 * * * *` |
| `@weekly` | `@weekly` | `0 0 * * 0` |
| `@monthly` | `@monthly` | `0 0 1 * *` |

### Disk pressure behaviour

| Threshold | Key | Default | Action |
|---|---|---|---|
| Emergency rotation | `DISK_CRITICAL_PERCENT` | `90` | Immediately rotates all jobs for that directory |
| Cloud panic backup | `CLOUD_BACKUP_ON_PANIC` | `false` | Ships archives to cloud after emergency rotation |
| Archive write guard | `DISK_MIN_FREE_MB` | `200` | Skips writing archive; source file preserved |

---

## Configuration Reference

| File | Purpose |
|---|---|
| `/etc/global-sys-utils/global.conf` | Global defaults for all jobs |
| `/etc/global-sys-utils/global.conf.d/*.conf` | Per-app rotation jobs (each file = one independent job in daemon mode) |

### Rotation keys

| Key | Default | Description |
|---|---|---|
| `LOG_DIR` | `/var/log/apps` | Directory to scan |
| `PATTERN` | `*.log` | Glob pattern |
| `OLD_LOGS_DIR` | `<logdir>/old_logs` | Archive output root |
| `EXCLUDE_FILE` | — | Path to file with one exclude glob per line |
| `PARALLEL_JOBS` | `4` | Concurrent rotations |
| `DATE_FORMAT` | `date` | `date` (YYYYMMDD) or `full` (timestamp) |
| `DRY_RUN` | `false` | Log actions without changes |
| `ENCRYPT` | `false` | AES-256-GCM encryption |

### Daemon + disk keys

| Key | Default | Description |
|---|---|---|
| `SCHEDULE` | — | Cron, interval, or `@alias` |
| `PID_FILE` | `/run/global-logrotate.pid` | PID file path |
| `DISK_CRITICAL_PERCENT` | `90` | Emergency rotation threshold |
| `DISK_MIN_FREE_MB` | `200` | Minimum free MB to write archive |
| `DISK_CHECK_INTERVAL` | `60` | Disk check interval (seconds) |

### Cloud backup keys

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
| `CLOUD_BACKUP_ON_SCHEDULE` | `false` | Run cloud backup after each rotation |
| `CLOUD_BACKUP_ON_PANIC` | `false` | Run cloud backup on disk-critical event |

### Logging keys

| Key | Default | Description |
|---|---|---|
| `LOG_FILE` | `/var/log/global-sys-utils/global-logrotate.log` | Log output path |
| `LOG_LEVEL` | `info` | `error` \| `info` \| `debug` |

### Full per-app example

```ini
# /etc/global-sys-utils/global.conf.d/nginx.conf
LOG_DIR       = /var/log/nginx
PATTERN       = *.log
SCHEDULE      = 0 2 * * *
PARALLEL_JOBS = 2

DISK_CRITICAL_PERCENT = 85
DISK_MIN_FREE_MB      = 500

CLOUD_PROVIDER           = aws
CLOUD_DESTINATION        = s3://my-bucket/nginx-logs
CLOUD_AWS_REGION         = us-east-1
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

| Command | Description |
|---|---|
| `make build` | Build binary for current architecture |
| `make build-all` | Build for amd64 and arm64 |
| `make deb` | Build DEB package (native arch) |
| `make deb GOARCH=arm64` | Build DEB for arm64 |
| `make rpm` | Build RPM package (native arch) |
| `make rpm GOARCH=amd64` | Build RPM for amd64 |
| `make packages-all` | Build all packages for all architectures |
| `make install` | Install to `/usr/bin/` and `/etc/` (requires root) |
| `make test` | Run Go + Python test suites |
| `make clean` | Remove build artifacts |

CI triggers automatically on push to `main` when files under `cmd/`, `packaging/`, `config/`, `completions/`, or `man/` change. Built packages are committed to `installers/v<VERSION>/` and a GitHub Release is created.

---

## Testing

```bash
make test
```

**Go tests** (57 tests, race-detector clean):

```bash
go test ./cmd/global-logrotate/ -race -v
```

Covers: cron/interval schedule parsing, gzip roundtrip, AES-256-GCM encryption roundtrip (incl. wrong-password rejection, bad magic bytes), rotation integration (basic, encrypted, dry-run, already-rotated idempotency, disk guard, parallel, permission stripping), `buildConfig` defaults, disk stats.

**Python tests** (41 tests, no cloud credentials required):

```bash
python3 -m pytest tests/test_utils.py -v
```

Covers: date extraction, S3/GCS URL parsing, object key construction, local path construction, MD5 hashing, `AdaptiveThrottle` concurrency cap, retry-on-failure logic. All cloud SDKs mocked.

---

## Maintenance

Daily maintenance is handled through GitHub automation and the repository maintenance policy.

- Dependabot checks Go modules, Python requirements, and GitHub Actions daily.
- Daily CI validates Go tests, Python tests, DEB/RPM package builds, and documentation consistency.
- Branch names stay short and area-based, for example `deps-go`, `deps-python`, `ci-fix`, `package-fix`, `docs-update`, `script-cleanup`, and `throughput-fix`.
- Dependency/library updates should be accepted when they fix security issues, compatibility issues, bugs, or clearly improve maintainability.
- Scripts should stay compact and simple; avoid replacing standard-library code unless a new library clearly reduces risk or complexity.
- Throughput optimizations must be measured and must preserve atomic writes, disk free-space guards, config compatibility, and package compatibility.
- Any behavior change must update matching README, man page, docs, package metadata, and website/docs content when present.

See [`docs/maintenance.md`](docs/maintenance.md) for the full policy.

---

## Project Structure

```
global-sys-utils/
├── cmd/
│   ├── global-logrotate/       # Go source — log rotation binary
│   │   ├── main.go             # ~1 800 lines: rotation, daemon, encryption
│   │   └── main_test.go        # 57 unit + integration tests
│   ├── global-aws-backup       # Python — upload aged logs to AWS S3
│   ├── global-aws-restore      # Python — download logs from AWS S3
│   ├── global-gcp-backup       # Python — upload aged logs to GCP GCS
│   └── global-gcp-restore      # Python — download logs from GCP GCS
├── completions/                # Bash and zsh shell completions
├── config/
│   ├── global.conf             # Documented main config (installed to /etc/)
│   └── global.conf.d/
│       └── example.conf        # Annotated per-app job template
├── docs/
│   └── maintenance.md          # Daily maintenance and PR policy
├── installers/                 # Pre-built .deb and .rpm packages per release
├── man/
│   └── global-logrotate.1      # Man page
├── packaging/
│   ├── deb/                    # Debian packaging (control, postinst, prerm)
│   ├── rpm/                    # RPM spec
│   └── systemd/                # systemd service and timer units
├── tests/
│   └── test_utils.py           # 41 Python utility tests
├── go.mod
├── requirements.txt
├── Makefile
└── LICENSE
```

---

## Documentation

| Resource | Description |
|----------|-------------|
| [config/global.conf](config/global.conf) | Annotated main configuration file with all keys and defaults |
| [config/global.conf.d/](config/global.conf.d/) | Per-app job config templates |
| [docs/maintenance.md](docs/maintenance.md) | Daily maintenance, branch, dependency, script, and documentation policy |
| [man/global-logrotate.1](man/global-logrotate.1) | Man page (`man global-logrotate` after install) |
| [packaging/systemd/](packaging/systemd/) | systemd service and timer unit files |
| [installers/](installers/) | Pre-built `.deb` and `.rpm` packages by release |
| [tests/test_utils.py](tests/test_utils.py) | Python utility test suite |

---

## Contributing

Contributions are welcome when they improve reliability, compatibility, security, performance, packaging, or documentation.

### How to contribute

1. **Fork** the repository
2. **Create a short branch** based on the change area, such as `deps-go`, `deps-python`, `ci-fix`, `package-fix`, `docs-update`, `script-cleanup`, or `throughput-fix`
3. **Make focused changes** and avoid unrelated rewrites in the same PR
4. **Update docs** when behavior, packaging, commands, config, or install steps change
5. **Run the tests** before opening a PR:
   ```bash
   make test
   go vet ./...
   ```
6. **Open a pull request** against `main` with a clear description of what changed and why

### What we welcome

- Security updates and dependency/library updates
- Package install/uninstall fixes
- systemd service and timer fixes
- Cloud backup/restore script improvements
- Measured throughput improvements to the Go rotation engine
- Compact script cleanups that reduce complexity
- Expanded test coverage
- Documentation and example updates

### Guidelines

- Keep `make test` passing, or fix tests as part of the change
- Keep scripts compact and readable
- Avoid speculative optimizations and cosmetic-only PRs
- For larger architecture changes, open an issue first to discuss
- No CLA, no contributor agreement — contributions are accepted under the project's MIT License

**Bug reports and feature requests:** open a [GitHub issue](https://github.com/rushikeshsakharleofficial/global-sys-utils/issues).

<a href="https://github.com/rushikeshsakharleofficial/global-sys-utils/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=rushikeshsakharleofficial/global-sys-utils" />
</a>

---

## Security

### Encryption

Log archives are encrypted with AES-256-GCM. The plaintext password is never stored — only its SHA-256 hash is written to `/etc/global-sys-utils/global.conf.d/encryption.conf`. The password is stored in `~/.global-sys-utils/config/credentials.ini` (mode `0600`).

```bash
global-logrotate --pass-gen     # initial setup
global-logrotate --pass-reset   # change password
```

Password resolution order: credentials file → `LOGROTATE_PASSWORD` env var → interactive prompt.

### Reporting vulnerabilities

Open a [GitHub Security Advisory](https://github.com/rushikeshsakharleofficial/global-sys-utils/security/advisories/new) for any security issue. Do not file public issues for vulnerabilities.

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.

---

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=rushikeshsakharleofficial/global-sys-utils&type=Date)](https://star-history.com/#rushikeshsakharleofficial/global-sys-utils&Date)

[Issues](https://github.com/rushikeshsakharleofficial/global-sys-utils/issues) · [Releases](https://github.com/rushikeshsakharleofficial/global-sys-utils/releases) · [License](LICENSE)

</div>
