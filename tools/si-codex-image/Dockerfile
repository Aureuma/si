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
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /home/si/.bashrc \
    && chown si:si /home/si/.bashrc

COPY entrypoint.sh /usr/local/bin/si-entrypoint
RUN chmod +x /usr/local/bin/si-entrypoint

ENTRYPOINT ["tini", "-s", "--", "/usr/local/bin/si-entrypoint"]
CMD ["bash", "-lc", "sleep infinity"]
