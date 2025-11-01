# RTSP Client Usage Guide

## Installation

### Prerequisites
- Go 1.21 or later
- Git

### Build from Source

```bash
# Clone the repository
git clone https://github.com/your-org/rtsp-client.git
cd rtsp-client

# Download dependencies
go mod download

# Build the binary
go build -o bin/rtsp-client ./cmd/rtsp-client/

# Optionally, install to $GOPATH/bin
go install ./cmd/rtsp-client/
```

## Basic Usage

### Command Line Options

```bash
rtsp-client -url <rtsp-url> [options]
```

#### Required Flags
- `-url`: RTSP stream URL (required)

#### Optional Flags
- `-output`: Output directory for frames (default: `./frames`)
- `-timeout`: Connection timeout (default: `10s`)
- `-verbose`: Enable verbose logging (default: `false`)

### Examples

#### Basic Usage
Connect to an RTSP stream and save frames to the default directory:

```bash
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream
```

#### Custom Output Directory
Save frames to a specific directory:

```bash
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream -output /data/frames
```

#### Verbose Mode
Enable detailed logging:

```bash
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream -verbose
```

#### Custom Timeout
Set a custom connection timeout:

```bash
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream -timeout 30s
```

#### Combined Options
```bash
./bin/rtsp-client \
  -url rtsp://192.168.1.100:554/stream \
  -output /data/camera1 \
  -timeout 15s \
  -verbose
```

## YAML Configuration File

The RTSP client supports YAML configuration files (similar to mediamtx) for easier configuration management. This is especially useful for production deployments where you want to store configuration separately from command-line arguments.

### Creating a Configuration File

1. **Copy the example configuration**:
   ```bash
   cp rtsp-client.yml.example rtsp-client.yml
   ```

2. **Edit the configuration file** with your settings:
   ```yaml
   rtsp_url: "rtsp://192.168.1.100:554/stream"
   output_dir: "./frames"
   timeout: "10s"
   verbose: false
   save_jpeg: true
   continuous_decoder: true
   ```

### Using the Configuration File

#### Basic Usage
```bash
./bin/rtsp-client rtsp-client.yml
```

#### Override YAML Settings with Flags
Command-line flags always override YAML values:
```bash
# Use YAML config but enable verbose logging
./bin/rtsp-client rtsp-client.yml -verbose

# Use YAML config but change output directory
./bin/rtsp-client rtsp-client.yml -output /custom/frames

# Use YAML config but override URL and timeout
./bin/rtsp-client rtsp-client.yml -url rtsp://other-server/stream -timeout 30s
```

### Configuration Priority

The priority order for configuration values is:
1. **Defaults** (built-in default values)
2. **YAML file** (overrides defaults)
3. **Command-line flags** (override YAML and defaults)

### Configuration File Options

| Option | YAML Key | Type | Default | Description |
|--------|----------|------|---------|-------------|
| RTSP URL | `rtsp_url` | string | - | RTSP stream URL (required) |
| Output Directory | `output_dir` | string | `./frames` | Directory for saved frames |
| Timeout | `timeout` | string | `10s` | Connection timeout (duration format) |
| Verbose | `verbose` | boolean | `false` | Enable verbose logging |
| Save JPEG | `save_jpeg` | boolean | `true` | Save frames as JPEG images (requires ffmpeg) |
| Continuous Decoder | `continuous_decoder` | boolean | `true` | Use continuous decoder (can decode P-frames) |

### Example Configuration Files

#### Minimal Configuration
```yaml
rtsp_url: "rtsp://192.168.1.100:554/stream"
```

#### Full Configuration
```yaml
rtsp_url: "rtsp://admin:password@192.168.1.100:554/stream"
output_dir: "/data/camera1/frames"
timeout: "30s"
verbose: true
save_jpeg: true
continuous_decoder: true
```

#### Production Configuration
```yaml
rtsp_url: "rtsp://camera.example.com:554/main_stream"
output_dir: "/var/lib/rtsp-client/frames"
timeout: "15s"
verbose: false
save_jpeg: true
continuous_decoder: true
```

### YAML File Location

You can place the YAML configuration file anywhere and reference it by path:
```bash
# Use absolute path
./bin/rtsp-client /etc/rtsp-client/config.yml

# Use relative path
./bin/rtsp-client ./configs/camera1.yml

# Use from any location
./bin/rtsp-client /home/user/rtsp-configs/production.yml
```

### Combining YAML with Examples Scripts

The example scripts support YAML configuration:

```bash
# Use YAML config with basic.sh
USE_YAML=true YAML_CONFIG=rtsp-client.yml RTSP_URL=rtsp://example.com/stream ./examples/basic.sh

# Use YAML config with record_session.sh
USE_YAML=true YAML_CONFIG=rtsp-client.yml DURATION=120 ./examples/record_session.sh
```

## RTSP URL Format

### Standard Format
```
rtsp://[username:password@]host[:port]/path
```

### Examples

#### Without Authentication
```
rtsp://192.168.1.100/stream
rtsp://192.168.1.100:554/live
rtsp://camera.example.com/video
```

#### With Authentication
```
rtsp://admin:password@192.168.1.100/stream
rtsp://user:pass123@camera.example.com:8554/live
```

#### Default Port
If no port is specified, the default RTSP port (554) is used:
```
rtsp://192.168.1.100/stream  # Uses port 554
```

## Output

### Frame Files
Frames are saved as individual H.264 files with timestamp-based naming:

```
frames/
├── 90000.h264
├── 93600.h264
├── 97200.h264
└── ...
```

#### Filename Format
- Format: `{timestamp}.h264`
- Timestamp: RTP timestamp from packet header
- Extension: `.h264` (Annex B byte stream format)

### Playing Back Frames

#### Using FFmpeg
Play a single frame:
```bash
ffplay -f h264 frames/90000.h264
```

#### Converting to Video
Combine frames into a video file:
```bash
# Note: This requires frames to be in sequence
ffmpeg -framerate 30 -i frames/%d.h264 -c copy output.mp4
```

#### Using VLC
```bash
vlc frames/90000.h264
```

## Monitoring

### Real-time Statistics
The client prints statistics every 5 seconds:

```
2024/01/15 10:30:00 Stats - Storage Stats: Total Frames: 150, Key Frames: 15, Total Bytes: 1048576 (1.00 MB)
```

### Frame Notifications
When verbose mode is enabled, the client logs each saved frame:

```
2024/01/15 10:30:00 Received: RTP Packet [Version: 2, PT: 96, Seq: 1000, TS: 90000, SSRC: 0x12345678, Marker: true, Payload: 1024 bytes]
2024/01/15 10:30:00 Decoded: Frame [Timestamp: 90000, Size: 4096 bytes, IsKey: true]
2024/01/15 10:30:00 Saved frame: 90000 (timestamp: 90000)
```

### Final Statistics
On shutdown, the client prints final statistics:

```
Final Statistics:
Storage Stats: Total Frames: 1500, Key Frames: 150, Total Bytes: 15728640 (15.00 MB)
```

## Graceful Shutdown

Press `Ctrl+C` to stop the client gracefully:

```
^C
2024/01/15 10:35:00 
Received interrupt signal. Shutting down gracefully...
2024/01/15 10:35:00 Sending TEARDOWN request...
2024/01/15 10:35:00 
Final Statistics:
Storage Stats: Total Frames: 1500, Key Frames: 150, Total Bytes: 15728640 (15.00 MB)
```

The client will:
1. Stop receiving new packets
2. Send TEARDOWN to the server
3. Close all connections
4. Display final statistics
5. Exit cleanly

## Troubleshooting

### Connection Issues

#### Error: "failed to connect to RTSP server"
- Verify the RTSP URL is correct
- Check network connectivity to the server
- Ensure the server is running and accepting connections
- Try increasing the timeout: `-timeout 30s`

#### Error: "DESCRIBE failed"
- Check if authentication is required
- Verify the stream path is correct
- Ensure the server supports RTSP

### Packet Issues

#### Error: "too many errors reading packets"
- Network may be unstable
- Check firewall rules for UDP ports 50000-50001
- Verify RTP packets are reaching the client
- Try running with `-verbose` to see detailed errors

### Storage Issues

#### Error: "failed to create output directory"
- Check write permissions for the output directory
- Ensure the parent directory exists
- Verify disk space is available

#### Error: "failed to write frame file"
- Check disk space
- Verify write permissions
- Ensure the filesystem supports the operation

### Performance Issues

#### High CPU Usage
- This is expected for high-resolution streams
- Consider running on a more powerful machine
- Multiple concurrent streams may require optimization

#### High Memory Usage
- Memory usage should be stable after initial buffering
- If memory grows continuously, report as a bug

## Testing

### Running Unit Tests
```bash
go test ./...
```

### Running Integration Tests
Integration tests require a real RTSP server:

```bash
# Set the RTSP URL
export RTSP_URL=rtsp://192.168.1.100:554/stream

# Run integration tests
go test -tags=integration ./test/...
```

### Test Coverage
```bash
go test -cover ./...
```

### Verbose Test Output
```bash
go test -v ./...
```

## Advanced Usage

### Running as a Service

#### Using systemd (Linux)
Create `/etc/systemd/system/rtsp-client.service`:

```ini
[Unit]
Description=RTSP Client
After=network.target

[Service]
Type=simple
User=rtsp
ExecStart=/usr/local/bin/rtsp-client -url rtsp://192.168.1.100/stream -output /data/frames
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable rtsp-client
sudo systemctl start rtsp-client
sudo systemctl status rtsp-client
```

#### Using Docker
Create `Dockerfile`:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o rtsp-client ./cmd/rtsp-client/

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/rtsp-client .
CMD ["./rtsp-client"]
```

Build and run:
```bash
docker build -t rtsp-client .
docker run -v /data/frames:/frames rtsp-client \
  -url rtsp://192.168.1.100/stream \
  -output /frames
```

### Scripted Usage

#### Bash Script Example (Command-line Flags)
```bash
#!/bin/bash

RTSP_URL="rtsp://192.168.1.100:554/stream"
OUTPUT_DIR="/data/camera1/$(date +%Y%m%d)"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Run client
./bin/rtsp-client \
  -url "$RTSP_URL" \
  -output "$OUTPUT_DIR" \
  -verbose

# Check exit code
if [ $? -eq 0 ]; then
  echo "RTSP client exited successfully"
else
  echo "RTSP client exited with error"
  exit 1
fi
```

#### Bash Script Example (YAML Configuration)
```bash
#!/bin/bash

CONFIG_FILE="/etc/rtsp-client/camera1.yml"
OUTPUT_DIR="/data/camera1/$(date +%Y%m%d)"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Run client with YAML config, overriding output directory
./bin/rtsp-client "$CONFIG_FILE" -output "$OUTPUT_DIR"

# Check exit code
if [ $? -eq 0 ]; then
  echo "RTSP client exited successfully"
else
  echo "RTSP client exited with error"
  exit 1
fi
```

## Support

### Reporting Issues
Please report issues on GitHub with:
- RTSP client version
- Go version
- Operating system
- Full error message
- RTSP URL (without credentials)
- Steps to reproduce

### Contributing
Contributions are welcome! Please see CONTRIBUTING.md for guidelines.

### License
This project is licensed under the MIT License. See LICENSE for details.


