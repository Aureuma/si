# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/critic ./cmd/critic

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates curl nodejs npm bash && rm -rf /var/lib/apt/lists/*
RUN npm i -g @openai/codex >/dev/null 2>&1 || true
RUN useradd -m -u 1000 critic
WORKDIR /workspace
COPY --from=builder /out/critic /usr/local/bin/critic
USER critic
ENTRYPOINT ["critic"]
