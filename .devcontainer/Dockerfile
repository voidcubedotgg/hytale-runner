FROM golang:1.26-bookworm

# oras CLI for pushing/pulling OCI artifacts from the dev shell.
ARG ORAS_VERSION=1.2.0
ARG TARGETARCH=arm64
RUN curl -fsSL "https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_${TARGETARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin oras \
    && oras version

# Handy extras for poking the registry.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl jq \
    && rm -rf /var/lib/apt/lists/*

# Temurin (Eclipse Adoptium) JRE 25 LTS - runtime for the game server jar.
# Adoptium API picks the latest GA build for the arch (arm64->aarch64, amd64->x64).
ENV JAVA_HOME=/opt/java/jre
RUN set -eux; \
    case "${TARGETARCH}" in \
      arm64) JRE_ARCH=aarch64 ;; \
      amd64) JRE_ARCH=x64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    mkdir -p "${JAVA_HOME}"; \
    curl -fsSL "https://api.adoptium.net/v3/binary/latest/25/ga/linux/${JRE_ARCH}/jre/hotspot/normal/eclipse" \
      | tar -xz -C "${JAVA_HOME}" --strip-components=1; \
    "${JAVA_HOME}/bin/java" -version
ENV PATH="${JAVA_HOME}/bin:${PATH}"

WORKDIR /workspace
