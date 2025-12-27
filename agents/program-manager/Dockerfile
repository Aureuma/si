# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY . .
RUN CGO_ENABLED=0 go build -o /out/program-manager ./main.go

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 pm
WORKDIR /workspace
COPY --from=builder /out/program-manager /usr/local/bin/program-manager
USER pm
ENTRYPOINT ["program-manager"]
