BINARY = velotool
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all pi linux windows darwin clean

all: linux

# Native build (whatever platform you're on)
build:
	go build $(LDFLAGS) -o $(BINARY) .

# Linux ARM64 (Raspberry Pi)
pi:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc \
		go build $(LDFLAGS) -o $(BINARY)-linux-arm64 .

# Linux x86_64
linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .

# Windows x86_64
windows:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		go build $(LDFLAGS) -o $(BINARY).exe .

# macOS ARM64
darwin:
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		go build $(LDFLAGS) -o $(BINARY)-darwin-arm64 .

# Build all targets
release: pi linux windows darwin

clean:
	rm -f $(BINARY) $(BINARY)-* $(BINARY).exe

# Run tests
test:
	go test ./...

# Install on current system
install:
	go install $(LDFLAGS) .
