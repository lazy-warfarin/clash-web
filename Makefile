.PHONY: dev build test release

dev:
	cd ui && pnpm dev

build:
	cd ui && pnpm build
	go build -o dist/clash-web ./cmd/clash-web

test:
	go test ./...
	cd ui && pnpm typecheck

release:
	bash scripts/build-release.sh
