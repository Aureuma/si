FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/resource-broker ./main.go

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 broker
WORKDIR /workspace
COPY --from=builder /out/resource-broker /usr/local/bin/resource-broker
EXPOSE 9091
USER broker
ENTRYPOINT ["resource-broker"]
