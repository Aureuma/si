# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -o /out/coder-agent ./cmd/agent

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates git curl && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 coder
WORKDIR /workspace
COPY --from=builder /out/coder-agent /usr/local/bin/coder-agent
EXPOSE 8080
USER coder
ENTRYPOINT ["coder-agent"]
