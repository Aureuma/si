FROM golang:1.24-bookworm AS builder
WORKDIR /src
COPY agents/codex-monitor/go.mod agents/codex-monitor/go.sum ./
COPY agents/shared /shared
RUN go mod download
COPY agents/codex-monitor/ ./
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/codex-monitor .

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates bash curl nodejs npm tini && rm -rf /var/lib/apt/lists/*
RUN npm i -g @openai/codex >/dev/null 2>&1 || true
WORKDIR /workspace/silexa
COPY --from=builder /out/codex-monitor /usr/local/bin/codex-monitor
ENTRYPOINT ["tini", "-s", "--"]
CMD ["/usr/local/bin/codex-monitor"]
