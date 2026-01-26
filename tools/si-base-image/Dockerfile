FROM node:22-bookworm

RUN apt-get update -y \
    && apt-get install -y --no-install-recommends ca-certificates curl git tini \
    && rm -rf /var/lib/apt/lists/*

RUN npm i -g @openai/codex

RUN curl -LsSf https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin sh

RUN usermod -l si node \
    && groupmod -n si node \
    && usermod -d /home/si -m si

RUN printf '%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' > /etc/profile.d/si-codex-alias.sh \
    && chmod 0644 /etc/profile.d/si-codex-alias.sh \
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /etc/bash.bashrc
