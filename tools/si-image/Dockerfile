# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS codex-stdout-parser-build
WORKDIR /src/tools/codex-stdout-parser
COPY tools/codex-stdout-parser/go.mod tools/codex-stdout-parser/go.sum ./
RUN go mod download
COPY tools/codex-stdout-parser/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/codex-stdout-parser .

FROM golang:1.25-bookworm AS codex-init-build
WORKDIR /src/tools/codex-init
COPY tools/codex-init/go.mod ./go.mod
COPY tools/codex-init/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/si-codex-init .

FROM golang:1.25-bookworm AS critic-build
WORKDIR /src
COPY agents/critic/go.mod agents/critic/go.sum ./
COPY agents/shared /shared
COPY agents/critic/cmd ./cmd
RUN go mod download
RUN go mod tidy
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/critic ./cmd/critic

FROM golang:1.25-bookworm AS si-build
WORKDIR /src/tools/si
COPY tools/si/go.mod tools/si/go.sum ./
COPY agents/shared /src/agents/shared
COPY tools/si ./
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/si .

FROM node:22-bookworm

RUN apt-get update -y \
    && apt-get install -y --no-install-recommends ca-certificates curl gnupg gh git socat tini tmux \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
    && chmod a+r /etc/apt/keyrings/docker.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update -y \
    && apt-get install -y --no-install-recommends docker-ce-cli \
    && rm -rf /var/lib/apt/lists/*

RUN npm i -g @openai/codex@0.101.0

RUN curl -LsSf https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin sh

RUN usermod -l si node \
    && groupmod -n si node \
    && usermod -d /home/si -m si

ENV HOME=/home/si
ENV CODEX_HOME=/home/si/.codex
WORKDIR /workspace

RUN printf '%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' > /etc/profile.d/si-codex-alias.sh \
    && chmod 0644 /etc/profile.d/si-codex-alias.sh \
    && printf '%s\n' 'set -g mouse on' 'set -g history-limit 200000' > /etc/tmux.conf \
    && chmod 0644 /etc/tmux.conf \
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
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /root/.bashrc \
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /home/si/.bashrc \
    && printf '\n%s\n' '[ -f /etc/profile.d/si-term-title.sh ] && . /etc/profile.d/si-term-title.sh' >> /home/si/.bashrc \
    && printf '\n%s\n' '[ -f /etc/profile.d/si-term-title.sh ] && . /etc/profile.d/si-term-title.sh' >> /etc/bash.bashrc \
    && chown si:si /home/si/.bashrc

RUN --mount=type=secret,id=si_host_codex_config,required=false \
    mkdir -p /home/si/.codex /root/.codex \
    && if [ -s /run/secrets/si_host_codex_config ]; then \
         cp /run/secrets/si_host_codex_config /home/si/.codex/config.toml \
         && cp /run/secrets/si_host_codex_config /root/.codex/config.toml \
         && chown si:si /home/si/.codex/config.toml \
         && chown root:root /root/.codex/config.toml \
         && chmod 0600 /home/si/.codex/config.toml /root/.codex/config.toml; \
       fi

COPY tools/si-image/entrypoint.sh /usr/local/bin/si-entrypoint
RUN chmod +x /usr/local/bin/si-entrypoint

COPY tools/codex-skills /opt/si/codex-skills

COPY --from=codex-stdout-parser-build /out/codex-stdout-parser /usr/local/bin/codex-stdout-parser
COPY --from=codex-init-build /out/si-codex-init /usr/local/bin/si-codex-init
COPY --from=critic-build /out/critic /usr/local/bin/critic
COPY --from=si-build /out/si /usr/local/bin/si

ENTRYPOINT ["tini", "-s", "--", "/usr/local/bin/si-entrypoint"]
CMD ["bash", "-lc", "sleep infinity"]
