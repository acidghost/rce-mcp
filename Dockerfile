# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b

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

FROM docker.io/library/ubuntu:26.04@sha256:2260313b31c8c011cd2eebe728008efac1b3982be73eb71348ea2648d2c0e09b

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
