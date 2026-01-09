FROM golang:1.22-bookworm AS parser-build
WORKDIR /src/tools/codex-stdout-parser
COPY tools/codex-stdout-parser/go.mod tools/codex-stdout-parser/go.sum ./
RUN go mod download
COPY tools/codex-stdout-parser/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/codex-stdout-parser .

FROM golang:1.22-bookworm AS codex-init-build
WORKDIR /src/tools/codex-init
COPY tools/codex-init/go.mod ./go.mod
COPY tools/codex-init/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/silexa-codex-init .

FROM node:22-bookworm
WORKDIR /workspace/apps
RUN apt-get update -y && apt-get install -y --no-install-recommends socat tini && rm -rf /var/lib/apt/lists/*
COPY --from=parser-build /out/codex-stdout-parser /usr/local/bin/codex-stdout-parser
COPY --from=codex-init-build /out/silexa-codex-init /usr/local/bin/silexa-codex-init
# Placeholder for interactive LLM tooling; install inside container as needed (e.g., npm i -g @openai/codex-cli once available).
ENTRYPOINT ["tini", "-s", "--"]
CMD ["tail", "-f", "/dev/null"]
