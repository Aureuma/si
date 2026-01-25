# Builder
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY agents/critic/go.mod agents/critic/go.sum ./
COPY agents/shared /shared
COPY agents/critic/cmd ./cmd
RUN go mod download
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/critic ./cmd/critic

FROM golang:1.25-bookworm AS codex-init-build
WORKDIR /src/tools/codex-init
COPY tools/codex-init/go.mod ./go.mod
COPY tools/codex-init/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/silexa-codex-init .

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates curl nodejs npm bash tini git && rm -rf /var/lib/apt/lists/*
RUN npm i -g @openai/codex >/dev/null 2>&1 || true
RUN useradd -m -u 1000 critic
RUN printf '%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' > /etc/profile.d/si-codex-alias.sh \
    && chmod 0644 /etc/profile.d/si-codex-alias.sh \
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /root/.bashrc \
    && printf '\n%s\n' 'alias codex="codex --dangerously-bypass-approvals-and-sandbox"' >> /home/critic/.bashrc \
    && chown critic:critic /home/critic/.bashrc
WORKDIR /workspace
COPY --from=builder /out/critic /usr/local/bin/critic
COPY --from=codex-init-build /out/silexa-codex-init /usr/local/bin/silexa-codex-init
USER critic
ENTRYPOINT ["tini", "-s", "--"]
CMD ["critic"]
