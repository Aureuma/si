# Homebrew Core Readiness

This doc tracks the source-formula path needed for eventual submission to Homebrew Core.

## Why separate from the tap formula

- Tap formula (`Aureuma/homebrew-si`) installs prebuilt release archives.
- Homebrew Core generally expects source builds in formulae.
- SI keeps a core-ready source formula template in-repo and refreshes it per release tag.
- The canonical SI version still comes from root `Cargo.toml` under `[workspace.package].version`; this formula output is generated from that source rather than being maintained as its own version authority.

## Refresh the core formula template

```bash
si build homebrew core \
  --output packaging/homebrew-core/si.rb
```

This command defaults to the current SI workspace version from root `Cargo.toml`. Pass `--version` only when rendering for a different already-chosen release tag.

This updates:

- source archive URL
- source archive SHA256
- build/install test stanza

## Submission checklist

1. Ensure release tag `vX.Y.0` is published.
2. Regenerate `packaging/homebrew-core/si.rb`.
3. Validate formula style/audit in a Homebrew-enabled environment.
4. Confirm the source tarball formula can build the Rust primary binary from `rust/crates/si-cli`.
5. Open PR to `Homebrew/homebrew-core` with `si.rb` and test evidence.

## Current tap formula automation

Tap formula updates are automated from `.github/workflows/cli-release-assets.yml` after release assets are uploaded.
