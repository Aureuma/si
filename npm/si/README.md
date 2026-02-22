# @aureuma/si

Install the SI CLI with npm:

```bash
npm install -g @aureuma/si
```

Then run:

```bash
si --help
```

## How it works

This package installs a lightweight launcher script. On first run it downloads
an official SI release archive for your platform from GitHub Releases,
verifies it against `checksums.txt`, caches the binary locally, and executes it.

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
