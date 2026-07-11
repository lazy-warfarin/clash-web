# syntax=docker/dockerfile:1.10

FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS ui-builder
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.28.0 --activate
COPY ui/package.json ui/pnpm-lock.yaml ./ui/
RUN pnpm --dir ui install --frozen-lockfile
COPY ui ./ui
COPY internal/webassets ./internal/webassets
RUN pnpm --dir ui build

FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm AS app-builder
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=ui-builder /src/internal/webassets/dist ./internal/webassets/dist
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build \
    -trimpath \
    -ldflags="-s -w -buildid= -X main.version=${VERSION}" \
    -o /out/clash-web ./cmd/clash-web

FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm AS mihomo-builder
ARG MIHOMO_VERSION=v1.19.27
ARG MIHOMO_COMMIT=5184081ac327394d9e15fa5d5f9f4a61e723fd94
ARG TARGETARCH
WORKDIR /src
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "${MIHOMO_VERSION}" https://github.com/MetaCubeX/mihomo.git . \
    && test "$(git rev-parse HEAD)" = "${MIHOMO_COMMIT}"
RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" go build \
    -tags with_gvisor \
    -trimpath \
    -ldflags="-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=${MIHOMO_VERSION} -X github.com/metacubex/mihomo/constant.BuildTime=container" \
    -o /out/mihomo . \
    && cp LICENSE /out/mihomo-LICENSE

FROM debian:bookworm-slim
ARG VERSION=dev
LABEL org.opencontainers.image.title="Clash Web" \
      org.opencontainers.image.description="Server-native web control plane for mihomo" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0 AND GPL-3.0"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates dumb-init gosu \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 clash-web \
    && useradd --system --uid 10001 --gid clash-web --home-dir /var/lib/clash-web --shell /usr/sbin/nologin clash-web \
    && install -d -o clash-web -g clash-web -m 0750 /var/lib/clash-web \
    && install -d -o root -g clash-web -m 0750 /run/clash-web /etc/clash-web /usr/share/doc/clash-web

COPY --from=app-builder /out/clash-web /usr/bin/clash-web
COPY --from=mihomo-builder /out/mihomo /usr/lib/clash-web/mihomo
COPY --from=mihomo-builder /out/mihomo-LICENSE /usr/share/doc/clash-web/mihomo-LICENSE
COPY LICENSE /usr/share/doc/clash-web/LICENSE
COPY packaging/config.yaml /etc/clash-web/config.yaml
COPY packaging/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/bin/clash-web /usr/lib/clash-web/mihomo /usr/local/bin/docker-entrypoint.sh

VOLUME ["/var/lib/clash-web"]
EXPOSE 8080 7890/tcp 7890/udp
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/usr/local/bin/docker-entrypoint.sh"]
