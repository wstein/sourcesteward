GO ?= go

.PHONY: build extension check check-static test fmt vet docs-check
# check-static is everything check runs except the Go tests, so CI can run the
# race suite once instead of repeating a plain pass.
check:
	@test -z "$$(gofmt -l cmd internal)" || { echo 'Run make fmt'; exit 1; }
	$(GO) test -timeout 40m ./...
	@$(MAKE) --no-print-directory check-static

check-static:
	@test -z "$$(gofmt -l cmd internal)" || { echo 'Run make fmt'; exit 1; }
	$(GO) vet ./...
	$(GO) mod verify
	python3 -B -m unittest discover -s scripts -p 'test_*.py'
	node --test scripts/test_theme.mjs
	python3 -B scripts/check-docs.py
	python3 -B scripts/check-architecture.py
	git diff --check

build:
	mkdir -p bin
	$(GO) build -o bin/stw ./cmd/stw

# Optional local gh extension directory; no user-profile installation.
extension:
	mkdir -p bin/gh-jj
	$(GO) build -o bin/gh-jj/gh-jj ./cmd/gh-jj

test:
	$(GO) test -timeout 40m ./...
fmt:
	gofmt -w cmd internal
vet:
	$(GO) vet ./...
docs-check:
	python3 -B scripts/check-docs.py

# Install once with: python3 scripts/install-hugo.py (network access required).
.PHONY: pages
pages:
	python3 -B scripts/package-pages.py dist/pages "$$(git rev-parse HEAD)"
	python3 -B scripts/check-site.py dist/pages "$$(git rev-parse HEAD)"
