FROM golang:1.22-bookworm AS builder

RUN apt-get update -y && apt-get install -y --no-install-recommends git make ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone https://github.com/docker/mcp-gateway .

# Build the docker-mcp binary (gateway + catalog commands).
ENV GOTOOLCHAIN=go1.24.6
RUN CGO_ENABLED=0 go build -o /out/docker-mcp ./cmd/docker-mcp

FROM golang:1.22-bookworm AS entrypoint-build
WORKDIR /src/entrypoint
COPY tools/mcp-gateway/entrypoint/go.mod ./go.mod
COPY tools/mcp-gateway/entrypoint/*.go ./
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/mcp-entrypoint .

FROM docker:26-cli

RUN apk add --no-cache ca-certificates tini

WORKDIR /workspace
EXPOSE 8088

COPY --from=builder /out/docker-mcp /usr/local/bin/docker-mcp
COPY --from=entrypoint-build /out/mcp-entrypoint /usr/local/bin/mcp-entrypoint

ENTRYPOINT ["tini", "-s", "--", "/usr/local/bin/mcp-entrypoint"]
CMD ["gateway", "run", "--transport", "streaming", "--port", "8088", "--catalog", "/catalog/catalog.yaml"]
