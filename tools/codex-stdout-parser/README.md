# codex-stdout-parser

Parses Codex CLI output into JSON events, with optional PTY execution.

## Quick start

Build:

```bash
go build -o bin/codex-stdout-parser
```

Parse stdin:

```bash
cat log.txt | bin/codex-stdout-parser
```

Run a command in a PTY and capture turn reports:

```bash
bin/codex-stdout-parser -command "codex" -prompt "help"
```

## Notes

- The parser emits one JSON object per turn on stdout.
- ANSI escape sequences are stripped by default; use `-strip-ansi=false` to keep them.
- `-prompt-regex` and `-end-regex` control turn boundaries.
