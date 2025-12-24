# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/router ./main.go

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 router
WORKDIR /workspace
COPY --from=builder /out/router /usr/local/bin/router
USER router
ENTRYPOINT ["router"]

