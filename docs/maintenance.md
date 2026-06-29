# Maintenance Policy

This project uses small, focused maintenance changes.

## Daily checks

Daily validation covers:

- Go dependency updates
- Python dependency updates
- GitHub Actions updates
- Go tests with race detector
- Python tests
- DEB and RPM package builds
- README, man page, packaging, and workflow consistency

## Patch rules

Create a branch and pull request only when there is a real change:

- dependency update
- failing test or workflow fix
- package install/uninstall issue
- systemd service or timer issue
- cloud backup or restore script bug
- security issue
- measured throughput improvement
- documentation mismatch caused by code or package changes

Avoid cosmetic-only changes and speculative rewrites.

## Branch names

Use short names based on the change area:

- deps-go
- deps-python
- ci-fix
- package-fix
- docs-update
- script-cleanup
- throughput-fix

## Script policy

Scripts should stay compact and readable. Prefer standard library features unless a new library clearly reduces risk, improves compatibility, or fixes a real user problem.

## Documentation policy

When behavior changes, update all matching docs in the same pull request:

- README.md
- man/global-logrotate.1
- docs/*.md
- package metadata
- website/docs content, if present
