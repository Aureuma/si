FROM node:22-bookworm
WORKDIR /workspace/apps
RUN apt-get update -y && apt-get install -y --no-install-recommends socat && rm -rf /var/lib/apt/lists/*
# Placeholder for interactive LLM tooling; install inside container as needed (e.g., npm i -g @openai/codex-cli once available).
CMD ["tail", "-f", "/dev/null"]
