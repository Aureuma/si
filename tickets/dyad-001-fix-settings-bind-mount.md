# DYAD-001 Fix Host Settings Bind Mount Inside Containers

## Problem

Inside dyad containers, the host settings file bind mount can appear as a directory:

- Expected: `/root/.si/settings.toml` is a regular file (read-only bind mount from the host)
- Observed: `/root/.si/settings.toml` is a directory, causing warnings like:
  - `warning: settings load failed: read settings /root/.si/settings.toml: is a directory`

This breaks or degrades commands that rely on settings (notably `si vault status` inside dyads).

## Hypothesis

Docker may create the target path as a directory when binding a host file to a container target path that does not already exist as a file.

## Goal

Ensure file bind mounts for host settings land as files for both root and si users:

- `/root/.si/settings.toml`
- `/home/si/.si/settings.toml`

## Acceptance Criteria

- After `si build image` and `si dyad spawn <name> --skip-auth`, in both actor and critic containers:
  - `test -f /root/.si/settings.toml` succeeds
  - `si vault status` does not warn about settings being a directory
- For codex containers (non-dyad), ensure:
  - `test -f /home/si/.si/settings.toml` succeeds

## Suggested Implementation

- Pre-create mount targets in the image build:
  - `mkdir -p /root/.si /home/si/.si`
  - `touch /root/.si/settings.toml /home/si/.si/settings.toml`
  - set safe permissions (e.g. `0600`) and ownership (`root:root` for `/root`, `si:si` for `/home/si`)

## Validation Steps

- `si build image`
- `si dyad spawn mountfix generic --skip-auth`
- `si dyad exec --member actor mountfix -- bash -lc 'ls -la /root/.si/settings.toml; si vault status | head -n 30'`
- `si dyad exec --member critic mountfix -- bash -lc 'ls -la /root/.si/settings.toml; si vault status | head -n 30'`

