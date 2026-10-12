# syntax=docker/dockerfile:1.27.1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM docker.io/library/golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
RUN apk add --no-cache git just
ARG BUILD_VERSION=SNAPSHOT-unknown
ARG BUILD_COMMIT=unknown
COPY . .
RUN just \
        version="${BUILD_VERSION}" \
        commit_sha="${BUILD_COMMIT}" \
        build_time="${BUILD_DATE}" \
        build

FROM docker.io/library/ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        findutils \
        gawk \
        git \
        grep \
        gzip \
        jq \
        make \
        python3 \
        ripgrep \
        sed \
        tar \
        unzip \
 && rm -rf /var/lib/apt/lists/*

RUN userdel ubuntu \
 && groupadd --gid 1000 rce \
 && useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash rce \
 && mkdir -p /workspace \
 && chown rce:rce /workspace

COPY --from=build /src/build/rce-mcp-linux-* /usr/local/bin/rce-mcp
RUN chmod 0755 /usr/local/bin/rce-mcp

USER rce:rce
WORKDIR /workspace
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD curl -fsS http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/rce-mcp"]
