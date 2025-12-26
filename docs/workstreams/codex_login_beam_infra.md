# Workstream: Codex Login Beam (Infra Dyad)

Owner: **Dyad Infra**  
Goal: Standardize Codex CLI logins for infra actor/critic using the Codex Login Beam, with reproducible tunnel/auth messaging and verification.

Tasks
1) Prep target
   - Identify container: `actor-infra` or `critic-infra`.
   - Ensure Docker socket access for socat sidecar (needed when codex binds 127.0.0.1).
2) Run Beam helper
   - `TELEGRAM_CHAT_ID=<id> bin/beam-codex-login.sh <actor-or-critic> [port] [ssh_target]`
   - This creates a Manager task and Telegram ping with the tunnel command.
3) Start Codex login
   - In the container: `codex login > /tmp/codex-login.out 2>&1 & echo $! > /tmp/codex-login.pid`
   - Capture from `/tmp/codex-login.out`: callback port (usually 1455) and full auth URL.
4) Handle localhost binding (if tunnel refuses)
   - Create forwarder in container netns:  
     `docker rm -f <name>-codex-forward 2>/dev/null || true`  
     `docker run -d --name <name>-codex-forward --network container:$(bin/docker-target.sh <actor-or-critic>) alpine/socat tcp-listen:1456,reuseaddr,fork tcp:127.0.0.1:<callback_port>`
   - Use forward port (1456) in the tunnel command: `ssh -N -L 127.0.0.1:<callback_port>:<container_ip>:1456 <ssh_target>`
5) Human message (Telegram + Manager task)
   - Format:
     ```
     Codex login (<container>):

     tunnel:
     ```shell
     <TUNNEL_COMMAND>
     ```

     URL:
     ```
     <AUTH_URL_FROM_CODEX_OUTPUT>
     ```
     ```
   - Log the exact message under `docs/beam_messages/`.
6) Verify and close
   - After human completes OAuth, in container: `bin/run-task.sh <actor-or-critic> codex login status` (or `codex whoami`).
   - Close the Manager task: `bin/complete-human-task.sh <id>`.
7) Recordkeeping
   - Update `docs/human_queue.md` if still pending.
   - Keep latest sent message and tunnel example in `docs/beam_messages/`.

References
- Beam definition and patterns: `docs/beams.md`
- Example messages: `docs/beam_messages/codex_login_actor_infra.txt`, `docs/beam_messages/codex_login_critic_infra.txt`
