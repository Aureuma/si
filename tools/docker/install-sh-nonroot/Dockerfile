FROM ubuntu:24.04

RUN apt-get update \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    coreutils \
    curl \
    findutils \
    gawk \
    git \
    gzip \
    sed \
    tar \
  && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --shell /bin/bash si
USER si
WORKDIR /home/si
ENV HOME=/home/si

COPY --chown=si:si run.sh /home/si/run.sh
RUN chmod +x /home/si/run.sh

ENTRYPOINT ["/home/si/run.sh"]
