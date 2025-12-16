FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /out/sample ./main.go

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /out/sample /usr/local/bin/sample
EXPOSE 8080
ENTRYPOINT ["sample"]
