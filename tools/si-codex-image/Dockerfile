FROM node:22-bookworm

RUN apt-get update -y \
    && apt-get install -y --no-install-recommends ca-certificates curl git gh tini \
    && rm -rf /var/lib/apt/lists/*

RUN npm i -g @openai/codex

RUN id -u si >/dev/null 2>&1 || useradd -m -s /bin/bash si
ENV HOME=/home/si
ENV CODEX_HOME=/home/si/.codex
WORKDIR /workspace

RUN printf '%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' > /etc/profile.d/si-codex-alias.sh \
    && chmod 0644 /etc/profile.d/si-codex-alias.sh \
    && printf '%s\n' 'if [ -n "${SI_TERM_TITLE:-}" ]; then' \
         '  __si_set_title() { printf "\\033]0;%s\\007" "$SI_TERM_TITLE"; }' \
         '  if [ -n "${PROMPT_COMMAND:-}" ]; then' \
         '    PROMPT_COMMAND="__si_set_title; ${PROMPT_COMMAND}"' \
         '  else' \
         '    PROMPT_COMMAND="__si_set_title"' \
         '  fi' \
         'fi' \
      > /etc/profile.d/si-term-title.sh \
    && chmod 0644 /etc/profile.d/si-term-title.sh \
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /home/si/.bashrc \
    && printf '\n%s\n' '[ -f /etc/profile.d/si-term-title.sh ] && . /etc/profile.d/si-term-title.sh' >> /home/si/.bashrc \
    && printf '\n%s\n' '[ -f /etc/profile.d/si-term-title.sh ] && . /etc/profile.d/si-term-title.sh' >> /etc/bash.bashrc \
    && chown si:si /home/si/.bashrc

COPY entrypoint.sh /usr/local/bin/si-entrypoint
RUN chmod +x /usr/local/bin/si-entrypoint

ENTRYPOINT ["tini", "-s", "--", "/usr/local/bin/si-entrypoint"]
CMD ["bash", "-lc", "sleep infinity"]
