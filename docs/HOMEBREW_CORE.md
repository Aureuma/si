# Homebrew Core Readiness

This doc tracks the source-formula path needed for eventual submission to Homebrew Core.

## Why separate from the tap formula

- Tap formula (`Aureuma/homebrew-si`) installs prebuilt release archives.
- Homebrew Core generally expects source builds in formulae.
- SI keeps a core-ready source formula template in-repo and refreshes it per release tag.

## Refresh the core formula template

```bash
tools/release/homebrew/render-core-formula.sh \
  --version vX.Y.Z \
  --output packaging/homebrew-core/si.rb
```

This updates:

- source archive URL
- source archive SHA256
- build/install test stanza

## Submission checklist

1. Ensure release tag `vX.Y.Z` is published.
2. Regenerate `packaging/homebrew-core/si.rb`.
3. Validate formula style/audit in a Homebrew-enabled environment.
4. Confirm `go build ./tools/si` succeeds from the source tarball context.
5. Open PR to `Homebrew/homebrew-core` with `si.rb` and test evidence.

## Current tap formula automation

Tap formula updates are automated from `.github/workflows/cli-release-assets.yml` after release assets are uploaded.
