FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/codex-monitor .

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates bash curl nodejs npm && rm -rf /var/lib/apt/lists/*
RUN npm i -g @openai/codex >/dev/null 2>&1 || true
WORKDIR /workspace/silexa
COPY --from=builder /out/codex-monitor /usr/local/bin/codex-monitor
ENTRYPOINT ["/usr/local/bin/codex-monitor"]
