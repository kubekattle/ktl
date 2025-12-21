# Packaging (deb/rpm)

This directory contains a Docker-based packaging workflow that builds Linux `deb` and `rpm` packages for `ktl` without requiring Ruby/FPM tooling on the host.

## Usage

```bash
make package
```

Artifacts are written into `./dist/` and should not be committed.

## Customization

- `PACKAGE_PLATFORMS` controls which Linux platforms to build (default: `linux/amd64`).
- `VERSION` and `LDFLAGS` are inherited from the Makefile defaults.

Example:

```bash
make package PACKAGE_PLATFORMS="linux/amd64 linux/arm64" VERSION=dev
```
