# Quick Start Guide

## Starting the RTSP Client Service

### Prerequisites

1. **Build the application** (first time only):
   ```bash
   make build
   ```

   Or manually:
   ```bash
   go build -o bin/rtsp-client ./cmd/rtsp-client/
   ```

2. **Verify FFmpeg is installed** (optional, for JPEG conversion):
   ```bash
   which ffmpeg
   # If not installed: brew install ffmpeg  # macOS
   ```

### Basic Usage

#### Method 1: Direct Command

```bash
# Start the service with your RTSP stream
./bin/rtsp-client -url rtsp://127.0.0.1:8554/mystream
```

#### Method 2: Using Makefile

```bash
# Start using Makefile
RTSP_URL=rtsp://127.0.0.1:8554/mystream make run
```

#### Method 3: Using YAML Configuration File

You can use a YAML configuration file (similar to mediamtx) for easier configuration management:

1. **Create a configuration file** from the example:
   ```bash
   cp rtsp-client.yml.example rtsp-client.yml
   ```

2. **Edit the configuration file** with your settings:
   ```yaml
   rtsp_url: "rtsp://127.0.0.1:8554/mystream"
   output_dir: "./frames"
   timeout: "10s"
   verbose: false
   save_jpeg: true
   continuous_decoder: true
   ```

3. **Run with the YAML file**:
   ```bash
   ./bin/rtsp-client rtsp-client.yml
   ```

4. **Override YAML values with command-line flags**:
   ```bash
   ./bin/rtsp-client rtsp-client.yml -verbose -output ./custom_frames
   ```

The priority order is: **Defaults < YAML < Command-line flags**

### Common Options

#### Basic Start (H.264 files only)
```bash
./bin/rtsp-client -url rtsp://127.0.0.1:8554/mystream -output ./frames
```

#### With JPEG Conversion (requires FFmpeg)
```bash
./bin/rtsp-client \
  -url rtsp://127.0.0.1:8554/mystream \
  -output ./frames \
  -jpeg
```

#### With Verbose Logging
```bash
./bin/rtsp-client \
  -url rtsp://127.0.0.1:8554/mystream \
  -output ./frames \
  -verbose
```

#### Frame-by-Frame Mode (keyframes only)
```bash
./bin/rtsp-client \
  -url rtsp://127.0.0.1:8554/mystream \
  -output ./frames \
  -continuous-decoder=false
```

### Full Command with All Options

```bash
./bin/rtsp-client \
  -url rtsp://127.0.0.1:8554/mystream \
  -output ./frames \
  -timeout 10s \
  -verbose \
  -jpeg \
  -continuous-decoder
```

### Command-Line Options

| Option | Description | Default |
|--------|-------------|---------|
| `-url` | RTSP stream URL (required) | - |
| `-output` | Output directory for frames | `./frames` |
| `-timeout` | Connection timeout | `10s` |
| `-verbose` | Enable verbose logging | `false` |
| `-jpeg` | Save frames as JPEG images | `true` (requires FFmpeg) |
| `-continuous-decoder` | Use continuous decoder (can decode P-frames) | `true` |

### Examples

#### Example 1: Basic Recording
```bash
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream
```

#### Example 2: Custom Output Directory
```bash
./bin/rtsp-client \
  -url rtsp://192.168.1.100:554/stream \
  -output /data/camera1/frames
```

#### Example 3: With Authentication
```bash
./bin/rtsp-client \
  -url rtsp://admin:password@192.168.1.100/stream \
  -output ./frames \
  -verbose
```

#### Example 4: Keyframes Only (Legacy Mode)
```bash
./bin/rtsp-client \
  -url rtsp://127.0.0.1:8554/mystream \
  -output ./frames \
  -jpeg \
  -continuous-decoder=false
```

#### Example 5: Using YAML Configuration
```bash
# Create config file
cp rtsp-client.yml.example rtsp-client.yml

# Edit rtsp-client.yml with your settings, then run:
./bin/rtsp-client rtsp-client.yml

# Override YAML settings with flags:
./bin/rtsp-client rtsp-client.yml -verbose
```

### Running in Background

To run the service in the background:

```bash
# Run in background
nohup ./bin/rtsp-client -url rtsp://127.0.0.1:8554/mystream -output ./frames > rtsp-client.log 2>&1 &

# View logs
tail -f rtsp-client.log

# Check if running
ps aux | grep rtsp-client

# Stop the service
pkill -f rtsp-client
```

### Stopping the Service

**Option 1: Press Ctrl+C** (if running in foreground)

**Option 2: Kill the process**
```bash
# Find the process
ps aux | grep rtsp-client

# Kill it
pkill -f rtsp-client
```

**Option 3: Graceful shutdown**
The service automatically handles SIGTERM and SIGINT signals for graceful shutdown.

### Checking Service Status

```bash
# Check if service is running
ps aux | grep rtsp-client

# Check output directory
ls -lh frames/

# View recent frames
ls -lt frames/ | head -10
```

### Output Files

The service saves frames in the output directory:

```
frames/
├── 498500574.h264    # H.264 frame file
├── 498500574.jpg     # JPEG image (if -jpeg enabled)
├── stream.h264        # Continuous stream file
└── corrupted_frames/ # Corrupted frames (if any)
    └── 498503574_corrupted.h264
```

### Troubleshooting

#### Service Won't Start
1. Check if RTSP URL is accessible:
   ```bash
   curl -v rtsp://127.0.0.1:8554/mystream
   ```

2. Check if port is available:
   ```bash
   netstat -an | grep 8554
   ```

3. Build the application:
   ```bash
   make build
   ```

#### No Frames Being Saved
1. Check verbose logs:
   ```bash
   ./bin/rtsp-client -url rtsp://127.0.0.1:8554/mystream -verbose
   ```

2. Verify RTSP stream is active and accessible

3. Check output directory permissions:
   ```bash
   ls -ld frames/
   ```

#### JPEG Conversion Not Working
1. Verify FFmpeg is installed:
   ```bash
   which ffmpeg
   ffmpeg -version
   ```

2. Install FFmpeg if missing:
   ```bash
   # macOS
   brew install ffmpeg
   
   # Linux
   sudo apt-get install ffmpeg
   ```

### Next Steps

- View saved frames: `ls -lh frames/`
- Convert H.264 frames to video: See [H264_TO_IMAGE.md](H264_TO_IMAGE.md)
- Analyze frame statistics: Check the console output every 5 seconds
- Review corrupted frames: Check `frames/corrupted_frames/` directory

