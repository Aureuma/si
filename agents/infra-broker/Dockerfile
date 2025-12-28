FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/infra-broker ./main.go

FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates tini && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 infrabot
WORKDIR /workspace
COPY --from=builder /out/infra-broker /usr/local/bin/infra-broker
EXPOSE 9092
USER infrabot
ENTRYPOINT ["tini", "-s", "--"]
CMD ["infra-broker"]
