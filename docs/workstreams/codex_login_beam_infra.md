# Workstream: Codex Login Beam (Infra Dyad)

Owner: **Dyad Infra**  
Goal: Standardize Codex CLI logins for infra actor/critic using the Codex Login Beam, with reproducible tunnel/auth messaging and verification.

Tasks
1) Prep target
   - Identify dyad pod: `bin/k8s-dyad-pod.sh <dyad>` (e.g., `infra`).
   - Ensure the actor image includes `socat` (required when codex binds 127.0.0.1).
2) Run Beam helper
   - `TELEGRAM_CHAT_ID=<id> bin/beam-codex-login.sh <dyad> [callback_port]`
   - This creates the Beam task; the Temporal workflow handles Codex login, forwarder setup, and Telegram notification.
3) Human action
   - Run the port-forward command from Telegram and open the auth URL.
4) Recordkeeping
   - Keep latest sent message and tunnel example in `docs/beam_messages/`.

References
- Beam definition and patterns: `docs/beams.md`
- Example messages: `docs/beam_messages/codex_login_actor_infra.txt`, `docs/beam_messages/codex_login_critic_infra.txt`
