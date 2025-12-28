FROM golang:1.22-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/dashboard .

FROM debian:bookworm-slim AS tini
RUN apt-get update -y && apt-get install -y --no-install-recommends tini && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/base-debian12
WORKDIR /workspace
COPY --from=tini /usr/bin/tini /usr/local/bin/tini
COPY --from=builder /out/dashboard /usr/local/bin/dashboard
EXPOSE 8087
ENTRYPOINT ["/usr/local/bin/tini", "-s", "--", "/usr/local/bin/dashboard"]
