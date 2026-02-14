# cbox justfile — build system for the cbox CLI

# Resolve version: git tag > short commit hash > "dev"
version := `git describe --tags --exact-match 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo dev`

ldflags := "-s -w -X main.version=" + version

# Build the cbox binary
build:
    go build -ldflags '{{ldflags}}' -o bin/cbox ./cmd/cbox

# Run all tests
test:
    go test ./...

# Install cbox to $GOPATH/bin
install:
    go install -ldflags '{{ldflags}}' ./cmd/cbox

# Remove build artifacts
clean:
    rm -rf bin/

# Print the version that would be embedded
show-version:
    @echo {{version}}
