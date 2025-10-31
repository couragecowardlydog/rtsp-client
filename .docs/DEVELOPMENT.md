# Development Guide

## Getting Started

### Prerequisites
- Go 1.21 or later
- Git
- Make (optional, but recommended)

### Setup Development Environment

1. Clone the repository:
```bash
git clone https://github.com/your-org/rtsp-client.git
cd rtsp-client
```

2. Download dependencies:
```bash
go mod download
# or
make deps
```

3. Run tests to verify setup:
```bash
go test ./...
# or
make test
```

## Project Structure

```
rtsp-client/
├── .docs/              # Documentation
│   ├── ARCHITECTURE.md # Architecture documentation
│   ├── USAGE.md       # Usage guide
│   └── DEVELOPMENT.md # This file
├── cmd/
│   └── rtsp-client/   # Main application
│       └── main.go
├── internal/          # Internal packages
│   └── config/        # Configuration management
├── pkg/               # Public packages
│   ├── decoder/       # H.264 decoder
│   ├── rtp/          # RTP packet parser
│   ├── rtsp/         # RTSP client
│   └── storage/      # Frame storage
├── test/             # Integration tests
│   └── integration_test.go
├── go.mod            # Go module file
├── go.sum            # Go dependencies
├── Makefile          # Build automation
└── README.md         # Project README
```

## Development Workflow

### 1. Create a Feature Branch
```bash
git checkout -b feature/my-new-feature
```

### 2. Write Tests First (TDD)
Following TDD principles, write tests before implementation:

```bash
# Create test file
touch pkg/mypackage/myfeature_test.go

# Write tests
# Implement feature
# Run tests
make test
```

### 3. Implement Feature
Write clean, well-documented code following Go best practices.

### 4. Format and Lint
```bash
make fmt
make vet
make lint  # If golangci-lint is installed
```

### 5. Run Tests
```bash
# Unit tests
make test

# With coverage
make test-coverage

# View coverage report
open coverage.html
```

### 6. Commit Changes
```bash
git add .
git commit -m "feat: add new feature"
```

### 7. Push and Create PR
```bash
git push origin feature/my-new-feature
# Create pull request on GitHub
```

## Testing

### Unit Tests
All packages should have comprehensive unit tests.

#### Writing Tests
```go
package mypackage

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyFunction(t *testing.T) {
    // Arrange
    input := "test"
    expected := "expected"

    // Act
    result := MyFunction(input)

    // Assert
    assert.Equal(t, expected, result)
}
```

#### Running Tests
```bash
# All tests
go test ./...

# Specific package
go test ./pkg/rtp/...

# Verbose
go test -v ./...

# With race detection
go test -race ./...

# With coverage
go test -cover ./...
```

### Integration Tests
Integration tests require a real RTSP server.

#### Running Integration Tests
```bash
export RTSP_URL=rtsp://192.168.1.100:554/stream
make test-integration
```

### Benchmarks
Add benchmarks for performance-critical code:

```go
func BenchmarkMyFunction(b *testing.B) {
    for i := 0; i < b.N; i++ {
        MyFunction("test")
    }
}
```

Run benchmarks:
```bash
go test -bench=. ./...
```

## Code Style

### Go Conventions
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Use meaningful variable names
- Add comments for exported functions
- Keep functions small and focused

### Error Handling
```go
// Good
if err != nil {
    return fmt.Errorf("failed to parse packet: %w", err)
}

// Bad
if err != nil {
    panic(err)
}
```

### Logging
```go
// Use log package for important events
log.Println("Starting RTSP client...")

// Use fmt for errors in tests
t.Errorf("expected %v, got %v", expected, actual)
```

### Comments
```go
// Package rtp provides RTP packet parsing functionality.
package rtp

// ParsePacket parses raw bytes into an RTP packet.
// It returns an error if the packet is malformed or has an invalid version.
func ParsePacket(data []byte) (*Packet, error) {
    // Implementation
}
```

## Building

### Development Build
```bash
make build
```

### Production Build
```bash
go build -ldflags="-s -w" -o bin/rtsp-client ./cmd/rtsp-client/
```

### Cross-Compilation
```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o bin/rtsp-client-linux ./cmd/rtsp-client/

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/rtsp-client.exe ./cmd/rtsp-client/

# macOS
GOOS=darwin GOARCH=amd64 go build -o bin/rtsp-client-macos ./cmd/rtsp-client/
```

## Debugging

### Using Delve
```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug
dlv debug ./cmd/rtsp-client/ -- -url rtsp://example.com/stream
```

### Debug Logging
Enable verbose mode:
```bash
./bin/rtsp-client -url rtsp://example.com/stream -verbose
```

### Network Debugging
Use Wireshark to capture RTSP/RTP traffic:
```bash
# Capture on all interfaces
sudo wireshark

# Filter for RTSP
rtsp

# Filter for RTP
rtp
```

## Performance Optimization

### Profiling
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.

# Memory profiling
go test -memprofile=mem.prof -bench=.

# View profile
go tool pprof cpu.prof
```

### Memory Usage
```bash
# Run with memory stats
GODEBUG=gctrace=1 ./bin/rtsp-client -url rtsp://example.com/stream
```

## Common Development Tasks

### Adding a New Package
1. Create package directory under `pkg/` or `internal/`
2. Create package file and test file
3. Write tests first
4. Implement functionality
5. Update documentation
6. Add examples if public package

### Adding a New RTSP Method
1. Add test in `pkg/rtsp/client_test.go`
2. Add method to `Client` struct
3. Update `buildRequest` if needed
4. Test with real RTSP server
5. Document in USAGE.md

### Modifying RTP Parser
1. Add test cases in `pkg/rtp/packet_test.go`
2. Update `ParsePacket` function
3. Verify with real RTP packets
4. Update documentation

### Adding Storage Backend
1. Create new package under `pkg/storage/`
2. Implement `Storage` interface
3. Add comprehensive tests
4. Update main.go to support new backend
5. Document in ARCHITECTURE.md

## Troubleshooting

### Tests Failing
```bash
# Clean and rebuild
make clean
make build

# Update dependencies
go mod tidy

# Run tests with verbose output
go test -v ./...
```

### Build Errors
```bash
# Check Go version
go version

# Verify dependencies
go mod verify

# Clean cache
go clean -cache -modcache -testcache
```

### Import Issues
```bash
# Update imports
go mod tidy

# Verify module path
grep module go.mod
```

## Continuous Integration

### GitHub Actions
The project uses GitHub Actions for CI/CD. The workflow:
1. Runs on every push and pull request
2. Tests on multiple Go versions
3. Runs linters and formatters
4. Generates coverage reports
5. Builds for multiple platforms

### Pre-commit Hooks
Set up pre-commit hooks:

```bash
cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
make fmt
make vet
make test
EOF

chmod +x .git/hooks/pre-commit
```

## Documentation

### Updating Documentation
- Architecture changes: Update `.docs/ARCHITECTURE.md`
- Usage changes: Update `.docs/USAGE.md`
- Development process: Update `.docs/DEVELOPMENT.md`
- README: Update `README.md`

### Generating Code Documentation
```bash
# Generate and serve documentation
godoc -http=:6060

# View at http://localhost:6060/pkg/github.com/rtsp-client/
```

## Release Process

### Version Bump
1. Update version in `cmd/rtsp-client/main.go`
2. Update CHANGELOG.md
3. Tag release:
```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### Creating Release Binaries
```bash
# Build for all platforms
GOOS=linux GOARCH=amd64 go build -o bin/rtsp-client-linux-amd64 ./cmd/rtsp-client/
GOOS=darwin GOARCH=amd64 go build -o bin/rtsp-client-darwin-amd64 ./cmd/rtsp-client/
GOOS=windows GOARCH=amd64 go build -o bin/rtsp-client-windows-amd64.exe ./cmd/rtsp-client/

# Create checksums
cd bin && sha256sum * > checksums.txt
```

## Getting Help

### Resources
- [Go Documentation](https://golang.org/doc/)
- [RTSP RFC 2326](https://tools.ietf.org/html/rfc2326)
- [RTP RFC 3550](https://tools.ietf.org/html/rfc3550)
- [H.264 Specification](https://www.itu.int/rec/T-REC-H.264)

### Community
- GitHub Issues: Report bugs and request features
- GitHub Discussions: Ask questions and share ideas

## Contributing

Please read CONTRIBUTING.md for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.


