.PHONY: all build test test-coverage test-integration clean run help install

# Variables
BINARY_NAME=rtsp-client
BUILD_DIR=bin
MAIN_PATH=./cmd/rtsp-client
GO=go
GOFLAGS=-v

# Default target
all: clean test build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run tests
test:
	@echo "Running unit tests..."
	$(GO) test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -cover -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@if [ -z "$(RTSP_URL)" ]; then \
		echo "Error: RTSP_URL environment variable not set"; \
		echo "Usage: RTSP_URL=rtsp://example.com/stream make test-integration"; \
		exit 1; \
	fi
	$(GO) test -v -tags=integration ./test/...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@rm -rf frames
	@echo "Clean complete"

# Run the application
run: build
	@if [ -z "$(RTSP_URL)" ]; then \
		echo "Error: RTSP_URL environment variable not set"; \
		echo "Usage: RTSP_URL=rtsp://example.com/stream make run"; \
		exit 1; \
	fi
	$(BUILD_DIR)/$(BINARY_NAME) -url $(RTSP_URL) $(ARGS)

# Install to $GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(MAIN_PATH)
	@echo "Install complete"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "Dependencies downloaded"

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Format complete"

# Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin v1.54.2" && exit 1)
	golangci-lint run ./...
	@echo "Lint complete"

# Run vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...
	@echo "Vet complete"

# Check for security issues
security:
	@echo "Running security check..."
	@which gosec > /dev/null || (echo "gosec not installed. Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest" && exit 1)
	gosec ./...
	@echo "Security check complete"

# Run all checks
check: fmt vet test
	@echo "All checks passed"

# Display help
help:
	@echo "RTSP Client - Makefile targets:"
	@echo ""
	@echo "  make build             - Build the application"
	@echo "  make test              - Run unit tests"
	@echo "  make test-coverage     - Run tests with coverage report"
	@echo "  make test-integration  - Run integration tests (requires RTSP_URL)"
	@echo "  make clean             - Clean build artifacts"
	@echo "  make run               - Build and run (requires RTSP_URL)"
	@echo "  make install           - Install to \$$GOPATH/bin"
	@echo "  make deps              - Download dependencies"
	@echo "  make fmt               - Format code"
	@echo "  make lint              - Run linter"
	@echo "  make vet               - Run go vet"
	@echo "  make security          - Run security checks"
	@echo "  make check             - Run all checks"
	@echo "  make help              - Display this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make test"
	@echo "  RTSP_URL=rtsp://192.168.1.100/stream make run"
	@echo "  RTSP_URL=rtsp://192.168.1.100/stream make run ARGS=\"-output /data/frames -verbose\""


