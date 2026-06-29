# Maintenance

`global-sys-utils` uses daily maintenance checks for dependencies, CI, packaging, scripts, and documentation.

## What runs daily

- Go module dependency checks
- Python dependency checks
- GitHub Actions dependency checks
- Go tests with race detector
- Python tests
- DEB and RPM package build validation
- Documentation consistency checks

## Patch policy

Changes should be small and focused. Open branches only for real maintenance work:

- dependency/library update
- failing workflow or test fix
- package install/uninstall fix
- systemd service or timer fix
- cloud backup/restore script bug
- security issue
- measured throughput improvement
- README, man page, or docs drift

## Documentation rule

When behavior changes, update matching docs in the same pull request:

- README.md
- man/global-logrotate.1
- docs/*.md
- website/docs/*.md
- package metadata
