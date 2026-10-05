VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
GO      ?= go
LDFLAGS := -s -w -X main.version=$(VERSION)
ARCHS   := amd64 arm64

.PHONY: build test dist deb release clean

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/vegasyncor ./cmd/vegasyncor

test:
	$(GO) vet ./...
	$(GO) test ./...

# static binaries for all architectures
dist:
	@for a in $(ARCHS); do \
		echo "→ linux/$$a"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$a $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/vegasyncor-linux-$$a ./cmd/vegasyncor || exit 1; \
	done

# .deb packages for Debian/Ubuntu
deb:
	GO=$(GO) packaging/build-deb.sh $(VERSION) $(ARCHS)

# all the files of a GitHub release in dist/release
release:
	GO=$(GO) packaging/build-release.sh $(VERSION)

clean:
	rm -rf bin dist
