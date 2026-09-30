GO ?= go
BIN := bin
# Build version shown by `botpanel version` and on the Diagnostics page.
VERSION ?= $(shell date -u +%Y.%m.%d)-dev

.PHONY: all web build test check integration e2e run-dev clean release

all: build

# Build the static frontend and copy it into the embed directory.
web:
	cd web && npm ci && npm run build
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -r web/build/. internal/webui/dist/

# Production binary (frontend embedded). Build the frontend first.
build: web
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o $(BIN)/botpanel ./cmd/botpanel

test:
	$(GO) vet ./...
	$(GO) test ./...

# Real-Docker tests. Needs a DISPOSABLE daemon:
#   BOTPANEL_TEST_DOCKER_HOST=unix:///path/docker.sock [BOTPANEL_TEST_IMAGE_PREFIX=mirror.gcr.io/library/] [BOTPANEL_TEST_ROOTLESS=1]
integration:
	$(GO) test -tags integration -count=1 -timeout 60m -v ./tests/integration

check: test
	cd web && npm run check

# Browser checks of the core workflows against a throwaway panel (no Docker
# needed). Needs a built binary and a local Chromium (see web/tests/e2e/run.mjs).
e2e: build
	cd web && npm run test:e2e

# Versioned Linux release archives (amd64, arm64) with the binary, the
# deployment files and the documentation, plus SHA256SUMS. Set VERSION.
RELEASE_ARCHES ?= amd64 arm64
release: web
	rm -rf dist && mkdir -p dist
	set -e; for arch in $(RELEASE_ARCHES); do \
		name=botpanel-$(VERSION)-linux-$$arch; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o dist/$$name/botpanel ./cmd/botpanel; \
		cp -r deploy dist/$$name/deploy; \
		cp -r docs dist/$$name/docs; \
		cp README.md dist/$$name/; \
		tar -C dist --owner=0 --group=0 --sort=name -czf dist/$$name.tar.gz $$name; \
		rm -rf dist/$$name; \
	done
	cd dist && sha256sum *.tar.gz > SHA256SUMS
	@cat dist/SHA256SUMS

# Run against ./.dev-data; needs no Docker or system directories.
run-dev:
	BOTPANEL_ENV=development $(GO) run ./cmd/botpanel

clean:
	rm -rf $(BIN) dist web/build web/.svelte-kit
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
