FROM ubuntu:24.04

ENV RUSTUP_HOME=/opt/rust/rustup
ENV CARGO_HOME=/opt/rust/cargo
ENV PATH=/opt/rust/cargo/bin:${PATH}

RUN apt-get update \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    ca-certificates \
    coreutils \
    curl \
    findutils \
    gawk \
    git \
    gzip \
    pkg-config \
    sed \
    tar \
  && rm -rf /var/lib/apt/lists/* \
  && mkdir -p /opt/rust \
  && curl https://sh.rustup.rs -sSf | sh -s -- -y --no-modify-path --profile minimal --default-toolchain 1.86.0 \
  && chmod -R a+rwx /opt/rust

COPY run.sh /usr/local/bin/run.sh
RUN chmod +x /usr/local/bin/run.sh

ENTRYPOINT ["/usr/local/bin/run.sh"]
