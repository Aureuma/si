# Ticket 0001: Add Dyad "Peek" (tmux)

## Problem

When a dyad is running, it should be easy to observe both the actor and critic interactive sessions while they are mid-turn.

## Acceptance Criteria

- `si dyad peek <dyad>` opens a host `tmux` session split into two panes:
  - actor session
  - critic session
- Works even if the dyad sessions are created after the command starts (wait/retry until `tmux has-session` succeeds).
- Supports `--member actor|critic|both` and `--detached`.

## Manual Test

```bash
si dyad spawn <dyad>
si dyad peek <dyad>
```

