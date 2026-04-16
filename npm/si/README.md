# @aureuma/si

Install the SI CLI with npm:

```bash
mkdir -p "$HOME/.npm-global"
npm config set prefix "$HOME/.npm-global"
export PATH="$HOME/.npm-global/bin:$PATH"
npm install -g @aureuma/si
```

Then run:

```bash
si --help
```

If you previously installed `si` into `/usr/local/bin`, remove that older binary or
ensure your user-owned npm prefix comes first on `PATH`, otherwise your shell may
continue launching the stale `/usr/local/bin/si`.

## How it works

This package installs a lightweight launcher script. On first run it downloads
an official SI release archive for your platform from GitHub Releases,
verifies it against `checksums.txt`, caches the binary locally, and executes it.
The checked-in `package.json` version is only a packaging placeholder; SI's real
release/package version is derived from root `Cargo.toml [workspace.package].version`
when the npm artifact is built.

Supported targets:

- macOS: `amd64`, `arm64`
- Linux: `amd64`, `arm64`, `armv7`

## Environment overrides

- `SI_NPM_GITHUB_REPO`: GitHub repo (`owner/name`) to fetch releases from.
  Default: `Aureuma/si`
- `SI_NPM_RELEASE_BASE_URL`: direct base URL for release assets.
- `SI_NPM_CACHE_DIR`: custom cache directory.
- `SI_NPM_LOCAL_ARCHIVE_DIR`: local directory containing release archives and
  `checksums.txt` (useful for testing).
