#!/bin/bash
#
# Basic example of using RTSP client
# This script demonstrates connecting to an RTSP stream and saving frames
#

set -e

# Configuration
RTSP_URL="${RTSP_URL:-rtsp://192.168.1.100:554/stream}"
OUTPUT_DIR="${OUTPUT_DIR:-./frames}"
TIMEOUT="${TIMEOUT:-10s}"
VERBOSE="${VERBOSE:-false}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Print colored message
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if binary exists
if [ ! -f "./bin/rtsp-client" ]; then
    print_error "rtsp-client binary not found. Building..."
    make build
fi

# Create output directory
print_info "Creating output directory: $OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# Check if RTSP_URL is set
if [ -z "$RTSP_URL" ]; then
    print_error "RTSP_URL is not set"
    print_info "Usage: RTSP_URL=rtsp://example.com/stream ./examples/basic.sh"
    exit 1
fi

# Build command
CMD="./bin/rtsp-client -url $RTSP_URL -output $OUTPUT_DIR -timeout $TIMEOUT"

if [ "$VERBOSE" = "true" ]; then
    CMD="$CMD -verbose"
fi

# Print configuration
print_info "Configuration:"
echo "  RTSP URL: $RTSP_URL"
echo "  Output Directory: $OUTPUT_DIR"
echo "  Timeout: $TIMEOUT"
echo "  Verbose: $VERBOSE"
echo ""

# Run client
print_info "Starting RTSP client..."
print_info "Press Ctrl+C to stop"
echo ""

$CMD

# Check exit code
EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
    print_info "RTSP client exited successfully"
    
    # Count frames
    FRAME_COUNT=$(ls -1 "$OUTPUT_DIR"/*.h264 2>/dev/null | wc -l)
    print_info "Total frames saved: $FRAME_COUNT"
    
    # Calculate total size
    if [ $FRAME_COUNT -gt 0 ]; then
        TOTAL_SIZE=$(du -sh "$OUTPUT_DIR" | cut -f1)
        print_info "Total size: $TOTAL_SIZE"
    fi
else
    print_error "RTSP client exited with code $EXIT_CODE"
    exit $EXIT_CODE
fi


