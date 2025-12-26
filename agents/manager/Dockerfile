# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/manager ./cmd/manager
RUN CGO_ENABLED=0 go build -o /out/manager-worker ./cmd/worker

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 manager
WORKDIR /workspace
COPY --from=builder /out/manager /usr/local/bin/manager
COPY --from=builder /out/manager-worker /usr/local/bin/manager-worker
EXPOSE 9090
USER manager
CMD ["manager"]
