# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags="-s -w -X github.com/ribeirogab/zapgate/internal/mcpserver.Version=${VERSION}" \
    -o /out/zapgate ./cmd/zapgate

FROM debian:trixie-20261020-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad
ARG USER_UID=1000
ARG USER_GID=1000
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates ffmpeg tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid "$USER_GID" zapgate \
    && useradd --uid "$USER_UID" --gid "$USER_GID" --no-create-home --home-dir /data --shell /usr/sbin/nologin zapgate \
    && install -d -o "$USER_UID" -g "$USER_GID" -m 0700 /data /run/zapgate
COPY --from=build /out/zapgate /usr/local/bin/zapgate
ENV HOME=/data
WORKDIR /data
USER ${USER_UID}:${USER_GID}
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["zapgate", "health"]
ENTRYPOINT ["zapgate"]
CMD ["serve"]
