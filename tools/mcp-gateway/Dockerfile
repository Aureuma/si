FROM golang:1.22-bookworm

RUN apt-get update -y && apt-get install -y --no-install-recommends git make ca-certificates docker.io && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone https://github.com/docker/mcp-gateway .

# Build the docker-mcp binary (gateway + catalog commands)
ENV GOTOOLCHAIN=go1.24.6
RUN go build -o /usr/local/bin/docker-mcp ./cmd/docker-mcp

WORKDIR /workspace
EXPOSE 8088
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["gateway", "run", "--transport", "streaming", "--port", "8088", "--catalog", "/catalog/catalog.yaml", "--enable-all-servers"]
