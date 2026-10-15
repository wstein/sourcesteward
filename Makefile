GO ?= go

.PHONY: build check test fmt vet docs-check
check:
	@test -z "$$(gofmt -l cmd internal)" || { echo 'Run make fmt'; exit 1; }
	$(GO) test ./...
	$(GO) vet ./...
	$(GO) mod verify
	python3 -B -m unittest discover -s scripts -p 'test_*.py'
	python3 -B scripts/check-docs.py
	python3 -B scripts/check-architecture.py
	git diff --check

build:
	mkdir -p bin
	$(GO) build -o bin/stw ./cmd/stw

test:
	$(GO) test ./...
fmt:
	gofmt -w cmd internal
vet:
	$(GO) vet ./...
docs-check:
	python3 -B scripts/check-docs.py
