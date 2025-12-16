# Human Action Queue

Append blocking tasks for humans here. Format:

- [ ] **Task**: <short description>
  - **Command(s) for human**: <exact commands to run locally>
  - **URL to open**: <URL>
  - **Window/Timeout**: <e.g., 15 minutes>
  - **Requested by**: <actor/critic name>
  - **Timestamp (UTC)**: <yyyy-mm-dd hh:mm:ss>
  - **Notes**: <extra context>

Mark done by changing `[ ]` to `[x]` with the finisher’s initials and time.

Example Codex CLI login task:
- [ ] **Task**: Complete Codex CLI OAuth for actor `silexa-actor-web`
  - **Command(s) for human**: `ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@host`
  - **URL to open**: `http://127.0.0.1:47123/auth/continue?...`
  - **Window/Timeout**: 15 minutes
  - **Requested by**: silexa-actor-web
  - **Timestamp (UTC)**: 2025-12-14 01:10:00
  - **Notes**: Run the command on a machine with a browser; keep tunnel alive until OAuth finishes.
