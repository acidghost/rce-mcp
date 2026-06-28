# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89

FROM docker.io/library/golang:1.26.4-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build
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

FROM docker.io/library/ubuntu:26.04@sha256:f3d28607ddd78734bb7f71f117f3c6706c666b8b76cbff7c9ff6e5718d46ff64

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
