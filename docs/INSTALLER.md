# Installer Survey And `si` Installer Design

This doc captures patterns from widely-used CLI installers (mostly Go CLIs) and how we apply those patterns to `si`'s installer.

## Survey (10 Examples)

1. Helm (`get-helm-3`)
   - Supports `curl` or `wget`.
   - Verifies downloads via SHA256, with optional GPG signature verification.
   - Uses `mktemp` + `trap` cleanup.
   - Uses `sudo` automatically when needed.

2. chezmoi (`get.chezmoi.io`)
   - Supports `curl` or `wget` and cleanly errors if neither exists.
   - Strong OS/arch mapping to Go’s `GOOS/GOARCH`.
   - Verifies downloads via checksums.
   - Uses `getopts` for flags and `mktemp`+`trap` for cleanup.

3. k3s (`get.k3s.io`)
   - “curl | sh” script with a large surface area and many env var knobs.
   - Strong defaults but emits explicit fatal errors when a platform is unsupported.
   - Uses `sudo` unless already root; heavy use of traps/cleanup.

4. k3d (`install.sh`)
   - Secure curl defaults (forces HTTPS/TLS, `--fail --show-error`).
   - Optional `--no-sudo`.
   - Resolves “latest” by following GitHub redirects.

5. rclone (`install.sh`)
   - Very defensive around missing decompression tools (`unzip` alternatives).
   - Uses `mktemp` in a portable way (GNU/BSD fallback).
   - Supports “beta” channel and version discovery.

6. kustomize (`install_kustomize.sh`)
   - Uses GitHub API to discover platform-specific release assets.
   - Clear “version not available for OS/arch” errors.
   - Uses `mktemp`+`trap` cleanup; supports `GITHUB_TOKEN` for rate limits/private.

7. golangci-lint (`install.sh`)
   - Uses a shared shell library style (robust OS/arch detection + SHA256 verification).
   - Works around real-world curl edge cases.
   - Good logging levels and consistent failure modes.

8. Task (`taskfile.dev/install.sh`)
   - Similar “download archive + checksum + extract + install” flow.
   - Strong portability: multiple SHA tools (`sha256sum`, `shasum`, etc.).

9. kubectl (official docs patterns)
   - Minimal installer: download single binary + download checksum + verify + `install`.
   - Puts verification ahead of privileged install steps.

10. ko (official docs patterns)
   - Release tarballs by OS/arch with a strong verification story (SLSA provenance).
   - Keeps installation steps explicit and reproducible.

## Takeaways (What Good Installers Do)

- Prefer non-root installs by default (`~/.local/bin`) and only use `sudo` when needed.
- Detect OS/arch carefully and map to Go conventions (`darwin/linux`, `amd64/arm64`).
- Make the “latest” version resolution reliable, and allow pinning a version/ref.
- Verify downloads when possible (SHA256 at a minimum; optionally signatures).
- Support both `curl` and `wget` where possible; error clearly if neither exists.
- Use `mktemp` and `trap` cleanup to avoid leaving junk behind.
- Keep output readable: `info` vs `warn` vs `error`, and provide actionable next steps.
- Keep the installed artifact minimal (install only the needed binary; build with `-trimpath` and stripped symbols when appropriate).

## Applying This To `si`

Constraints/needs:
- Must work on Intel macOS (Monterey+) and Debian/Ubuntu.
- Must handle “developer install” (build from source) because private repos and fast iteration matter.
- Must be robust when `go` is missing (bootstrap a compatible Go toolchain user-locally).
- Must produce a slim `si` binary artifact by default.
- Must give clear warnings when optional dependencies are missing (e.g. Docker BuildKit/buildx for image builds).

Design choices:
- Keep `tools/install-si.sh` as the primary entrypoint.
- Default install location:
  - Root: `/usr/local/bin/si`
  - Non-root: first writable of `~/.local/bin/si`, then `~/bin/si`
- Source build path:
  - Use local checkout when available, else `git clone` a repo URL and checkout a ref.
  - If `git` is missing, fall back to downloading a GitHub source tarball (best-effort).
  - Ensure a minimum Go version (parsed from `tools/si/go.mod`), downloading Go to a user-local cache when needed.
  - Build with `-trimpath -buildvcs=false` and stripped symbols (`-ldflags "-s -w"`) for a smaller binary.
- Optional extras:
  - Detect Docker buildx availability and attempt a user-level plugin install in interactive TTY mode; otherwise warn.
  - If Go was auto-downloaded, symlink `go`/`gofmt` next to `si` so `si build self` works on fresh machines.

## Critique (Known Limitations / Tradeoffs)

- Building from source is slower than downloading a prebuilt release binary and requires `git` and a Go toolchain (even if bootstrapped).
- Download verification is strong for Go toolchain downloads (official sha256), but we don’t attempt signature verification for `si` itself.
- We intentionally avoid editing shell rc files; we only print exact `PATH` lines.
- Installing Docker buildx is optional and user-scoped; system-wide installation is distro-specific and not handled automatically.

## Test and CI Contract

- Host smoke test: `./tools/test-install-si.sh`
  - Validates dry-run behavior, install/uninstall flows, and key edge-case regressions.
- Docker smoke test: `./tools/test-install-si-docker.sh`
  - Validates installer behavior in root and non-root containers using the local repo checkout.
- GitHub Actions workflow: `.github/workflows/install-smoke.yml`
  - Runs host smoke checks on `ubuntu-latest` and `macos-latest`.
  - Runs Docker smoke checks on Ubuntu.

This layered approach mirrors the OpenClaw pattern: keep fast host regressions and add
containerized smoke tests so installer behavior remains stable across permission models.
