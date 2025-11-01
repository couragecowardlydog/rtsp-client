#!/bin/bash
#
# Record a timed session from an RTSP stream
# This script records for a specified duration and then converts frames to video
# Supports both command-line flags and YAML configuration file
#

set -e

# Configuration
RTSP_URL="${RTSP_URL:-rtsp://192.168.1.100:554/stream}"
DURATION="${DURATION:-60}"  # Duration in seconds
SESSION_NAME="${SESSION_NAME:-session_$(date +%Y%m%d_%H%M%S)}"
OUTPUT_DIR="./recordings/$SESSION_NAME"
VERBOSE="${VERBOSE:-false}"
USE_YAML="${USE_YAML:-false}"  # Set to true to use YAML config file
YAML_CONFIG="${YAML_CONFIG:-rtsp-client.yml}"  # Path to YAML config file

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check dependencies
if ! command -v timeout &> /dev/null; then
    print_error "timeout command not found. Please install coreutils."
    exit 1
fi

# Build if necessary
if [ ! -f "./bin/rtsp-client" ]; then
    print_info "Building RTSP client..."
    make build
fi

# Create output directory
print_info "Creating session directory: $OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# Print configuration
print_info "Recording Configuration:"
if [ "$USE_YAML" = "true" ]; then
    echo "  Config Mode: YAML file ($YAML_CONFIG)"
    echo "  RTSP URL: $RTSP_URL (from YAML or override)"
else
    echo "  Config Mode: Command-line flags"
    echo "  RTSP URL: $RTSP_URL"
fi
echo "  Duration: ${DURATION}s"
echo "  Session: $SESSION_NAME"
echo "  Output: $OUTPUT_DIR"
echo ""

# Build command
if [ "$USE_YAML" = "true" ]; then
    # Check if YAML config file exists
    if [ ! -f "$YAML_CONFIG" ]; then
        print_warn "YAML config file not found: $YAML_CONFIG"
        print_info "Creating example YAML config from template..."
        if [ -f "../rtsp-client.yml.example" ]; then
            cp ../rtsp-client.yml.example "$YAML_CONFIG"
            # Update RTSP URL in YAML file
            sed -i.bak "s|rtsp_url:.*|rtsp_url: \"$RTSP_URL\"|" "$YAML_CONFIG" 2>/dev/null || \
            sed -i '' "s|rtsp_url:.*|rtsp_url: \"$RTSP_URL\"|" "$YAML_CONFIG" 2>/dev/null || true
            print_info "Created $YAML_CONFIG - please edit it with your settings"
        else
            print_error "YAML config template not found. Please create $YAML_CONFIG manually."
            exit 1
        fi
    fi
    
    # Use YAML config file, but override output directory for this session
    CMD="./bin/rtsp-client $YAML_CONFIG -output $OUTPUT_DIR"
    
    # Allow command-line flags to override YAML values
    if [ "$VERBOSE" = "true" ]; then
        CMD="$CMD -verbose"
    fi
    if [ -n "$RTSP_URL" ] && [ "$RTSP_URL" != "rtsp://192.168.1.100:554/stream" ]; then
        CMD="$CMD -url $RTSP_URL"
    fi
else
    # Use command-line flags
    CMD="./bin/rtsp-client -url $RTSP_URL -output $OUTPUT_DIR"
    if [ "$VERBOSE" = "true" ]; then
        CMD="$CMD -verbose"
    fi
fi

# Start recording
print_info "Starting recording for ${DURATION} seconds..."
print_info "Press Ctrl+C to stop early"
echo ""

timeout ${DURATION}s $CMD || true

# Check if frames were captured
FRAME_COUNT=$(ls -1 "$OUTPUT_DIR"/*.h264 2>/dev/null | wc -l)
if [ $FRAME_COUNT -eq 0 ]; then
    print_error "No frames were captured"
    exit 1
fi

print_info "Recording complete. Captured $FRAME_COUNT frames"

# Calculate statistics
TOTAL_SIZE=$(du -sh "$OUTPUT_DIR" | cut -f1)
print_info "Total size: $TOTAL_SIZE"

# Ask user if they want to convert to video
echo ""
read -p "Convert frames to video? (requires ffmpeg) [y/N]: " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    if ! command -v ffmpeg &> /dev/null; then
        print_error "ffmpeg not found. Please install ffmpeg."
        exit 1
    fi
    
    VIDEO_FILE="./recordings/${SESSION_NAME}.mp4"
    print_info "Converting frames to video: $VIDEO_FILE"
    
    # Create a list of frames sorted by timestamp
    cd "$OUTPUT_DIR"
    ls -1 *.h264 | sort -n > frames.txt
    
    # Concatenate frames
    print_info "Concatenating frames..."
    cat $(cat frames.txt) > combined.h264
    
    # Convert to MP4
    print_info "Encoding to MP4..."
    ffmpeg -f h264 -i combined.h264 -c:v copy -y "../${SESSION_NAME}.mp4" 2>&1 | tail -n 5
    
    if [ $? -eq 0 ]; then
        print_info "Video created successfully: $VIDEO_FILE"
        
        # Clean up temporary files
        rm frames.txt combined.h264
        
        # Ask if user wants to keep individual frames
        echo ""
        read -p "Keep individual frame files? [y/N]: " -n 1 -r
        echo ""
        
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_info "Removing individual frames..."
            rm -rf "$OUTPUT_DIR"
        fi
    else
        print_error "Failed to create video"
        exit 1
    fi
fi

print_info "Session complete!"


