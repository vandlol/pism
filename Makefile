BINARY := pism
DIST := dist
PKG := github.com/vandlol/pism

# Version: current git tag/description, overridable: make VERSION=1.2.3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build dist install vet test clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build
	install -m0755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

dist:
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(DIST)/pism-$$os-$$arch; \
		[ "$$os" = "windows" ] && out=$$out.exe; \
		echo "building $$os/$$arch -> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf $(BINARY) $(DIST)
