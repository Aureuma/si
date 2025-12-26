# Workstream: Codex Login Beam (Infra Dyad)

Owner: **Dyad Infra**  
Goal: Standardize Codex CLI logins for infra actor/critic using the Codex Login Beam, with reproducible tunnel/auth messaging and verification.

Tasks
1) Prep target
   - Identify dyad pod: `bin/k8s-dyad-pod.sh <dyad>` (e.g., `infra`).
   - Ensure the actor image includes `socat` (required when codex binds 127.0.0.1).
2) Run Beam helper
   - `TELEGRAM_CHAT_ID=<id> bin/beam-codex-login.sh <dyad> [callback_port]`
   - This posts the `kubectl port-forward` command + auth URL to Telegram.
3) Start Codex login
   - In the container: `codex login > /tmp/codex-login.out 2>&1 & echo $! > /tmp/codex-login.pid`
   - Capture from `/tmp/codex-login.out`: callback port (usually 1455) and full auth URL.
4) Handle localhost binding
   - `bin/beam-codex-login.sh` starts a `socat` forwarder inside the actor pod so the `kubectl port-forward` works even when codex binds localhost.
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
   - After human completes OAuth, in container: `bin/run-task.sh <dyad> codex login status` (or `codex whoami`).
   - Close the Manager task: `bin/complete-human-task.sh <id>`.
7) Recordkeeping
   - Update `docs/human_queue.md` if still pending.
   - Keep latest sent message and tunnel example in `docs/beam_messages/`.

References
- Beam definition and patterns: `docs/beams.md`
- Example messages: `docs/beam_messages/codex_login_actor_infra.txt`, `docs/beam_messages/codex_login_critic_infra.txt`
