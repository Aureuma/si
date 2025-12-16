# Builder
FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/telegram-bot ./main.go

# Runtime
FROM debian:bookworm-slim
RUN apt-get update -y && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 telebot
WORKDIR /workspace
COPY --from=builder /out/telegram-bot /usr/local/bin/telegram-bot
EXPOSE 8081
USER telebot
ENTRYPOINT ["telegram-bot"]
