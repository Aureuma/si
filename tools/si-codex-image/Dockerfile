FROM node:22-bookworm

RUN apt-get update -y \
    && apt-get install -y --no-install-recommends ca-certificates curl git gh tini \
    && rm -rf /var/lib/apt/lists/*

RUN npm i -g @openai/codex

RUN useradd -m -u 1000 si
ENV HOME=/home/si
ENV CODEX_HOME=/home/si/.codex
WORKDIR /workspace

COPY entrypoint.sh /usr/local/bin/si-entrypoint
RUN chmod +x /usr/local/bin/si-entrypoint

ENTRYPOINT ["tini", "-s", "--", "/usr/local/bin/si-entrypoint"]
CMD ["bash", "-lc", "sleep infinity"]
