FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/credentials-mcp ./main.go

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates curl tini && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /usr/local/bin/sops https://github.com/getsops/sops/releases/download/v3.9.0/sops-v3.9.0.linux.amd64 \
  && chmod +x /usr/local/bin/sops

WORKDIR /workspace
COPY --from=builder /out/credentials-mcp /usr/local/bin/credentials-mcp

ENTRYPOINT ["tini", "-s", "--"]
CMD ["credentials-mcp"]
