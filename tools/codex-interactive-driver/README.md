# codex-interactive-driver

`codex-interactive-driver` drives interactive Codex-compatible REPLs over a PTY using explicit scripted actions.

## Build

```bash
go build ./tools/codex-interactive-driver
```

## Script actions

- `wait_prompt[:duration]`
- `send:<line>`
- `type:<text>`
- `key:<enter|tab|esc|up|down|left|right|ctrl-c>`
- `sleep:<duration>`
- `wait_contains:<substring>[|duration]`

## Example

```bash
cat > /tmp/codex-drive.steps <<'STEPS'
wait_prompt:20s
send:/status
wait_contains:status
wait_prompt:20s
send:/exit
wait_contains:bye
STEPS

./codex-interactive-driver \
  -command 'codex --dangerously-bypass-approvals-and-sandbox' \
  -script /tmp/codex-drive.steps \
  -print-output
```
