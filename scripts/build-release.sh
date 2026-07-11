#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.1.0}"
VERSION="${VERSION#v}"
MIHOMO_VERSION="v1.19.27"
MIHOMO_COMMIT="5184081ac327394d9e15fa5d5f9f4a61e723fd94"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" log -1 --format=%ct)}"
export SOURCE_DATE_EPOCH

command -v pnpm >/dev/null
command -v go >/dev/null
command -v nfpm >/dev/null

pnpm --dir "$ROOT/ui" install --frozen-lockfile
pnpm --dir "$ROOT/ui" build

MIHOMO_DIR="$ROOT/.cache/mihomo-$MIHOMO_VERSION"
if [[ ! -d "$MIHOMO_DIR/.git" ]]; then
  mkdir -p "$ROOT/.cache"
  git clone --depth 1 --branch "$MIHOMO_VERSION" https://github.com/MetaCubeX/mihomo.git "$MIHOMO_DIR"
fi
ACTUAL_COMMIT="$(git -C "$MIHOMO_DIR" rev-parse HEAD)"
if [[ "$ACTUAL_COMMIT" != "$MIHOMO_COMMIT" ]]; then
  echo "mihomo tag mismatch: expected $MIHOMO_COMMIT, got $ACTUAL_COMMIT" >&2
  exit 1
fi

for GOARCH in amd64 arm64; do
  export GOARCH
  case "$GOARCH" in amd64) export ARCH=amd64 GOAMD64=v1 ;; arm64) export ARCH=arm64; unset GOAMD64 || true ;; esac
  OUT="$ROOT/dist/$GOARCH"
  mkdir -p "$OUT"
  CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=true -ldflags "-s -w -X main.version=$VERSION -buildid=" -o "$OUT/clash-web" "$ROOT/cmd/clash-web"
  (
    cd "$MIHOMO_DIR"
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -tags with_gvisor -trimpath -ldflags "-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=$MIHOMO_VERSION -X 'github.com/metacubex/mihomo/constant.BuildTime=@${SOURCE_DATE_EPOCH}'" -o "$OUT/mihomo" .
  )
  cp "$MIHOMO_DIR/LICENSE" "$OUT/mihomo-LICENSE"
  {
    echo "clash-web=$VERSION"
    echo "mihomo=$MIHOMO_VERSION"
    echo "mihomo_commit=$MIHOMO_COMMIT"
    echo "architecture=$GOARCH"
    echo "clash_web_sha256=$(sha256sum "$OUT/clash-web" | cut -d' ' -f1)"
    echo "mihomo_sha256=$(sha256sum "$OUT/mihomo" | cut -d' ' -f1)"
  } > "$OUT/BUILD-INFO"
  (cd "$ROOT" && VERSION="$VERSION" nfpm package --config packaging/nfpm.yaml --packager deb --target "$OUT/")
done
