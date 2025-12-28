FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/releaseparty-api ./cmd/releaseparty-api

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 releaseparty
WORKDIR /app
RUN mkdir -p /app/data && chown -R releaseparty:releaseparty /app
COPY --from=builder /out/releaseparty-api /usr/local/bin/releaseparty-api
ENV RP_ADDR=:8080
ENV RP_DB_PATH=/app/data/releaseparty.sqlite
EXPOSE 8080
USER releaseparty
ENTRYPOINT ["releaseparty-api"]
