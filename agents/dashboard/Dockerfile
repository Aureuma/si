FROM golang:1.22-bookworm AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/dashboard .

FROM gcr.io/distroless/base-debian12
WORKDIR /workspace
COPY --from=builder /out/dashboard /usr/local/bin/dashboard
EXPOSE 8087
ENTRYPOINT ["/usr/local/bin/dashboard"]

