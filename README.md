# RTSP Client

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/Tests-Passing-success)](https://github.com/rtsp-client)

A production-ready, test-driven RTSP client written in Go from scratch. Connects to RTSP streams, receives RTP packets, decodes H.264 video frames, and saves them with timestamp-based naming.

## ⚠️ Disclaimer

**This repository contains AI-generated code. Use it at your own risk.**

This software is provided "as is", without warranty of any kind. While efforts have been made to ensure functionality, users should:
- Review the code before use in production environments
- Test thoroughly in their specific use cases
- Not rely solely on this software for critical applications
- Report any issues or bugs found

The maintainers assume no liability for any damages arising from the use of this software.

## ✨ Features

- 🎥 **Full RTSP Protocol Support**: DESCRIBE, SETUP, PLAY, TEARDOWN
- 📦 **RTP Packet Parsing**: Complete RFC 3550 implementation
- 🎬 **H.264 Decoder**: NAL unit assembly and FU-A defragmentation
- ⏱️ **Timestamp-Based Naming**: Frames named using RTP timestamp
- 🧪 **Test-Driven Development**: Comprehensive unit and integration tests
- 🏗️ **Clean Architecture**: Modular, maintainable, and extensible
- 🛡️ **Production-Ready**: Robust error handling and graceful shutdown
- 📊 **Real-time Statistics**: Frame count, keyframes, and data volume

## 🚀 Quick Start

### Prerequisites
- Go 1.21 or later
- Git

### Installation

```bash
# Clone the repository
git clone https://github.com/YOUR_USERNAME/rtsp-client.git
cd rtsp-client

# Build the binary
make build

# Or install directly
go install ./cmd/rtsp-client/
```

### Basic Usage

```bash
# Connect to an RTSP stream
./bin/rtsp-client -url rtsp://127.0.0.1:8554/stream

# Specify output directory
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream -output ./frames

# Enable verbose logging
./bin/rtsp-client -url rtsp://192.168.1.100:554/stream -verbose
```

## 📖 Documentation

- **[Usage Guide](.docs/USAGE.md)** - Comprehensive usage instructions and examples
- **[Architecture](.docs/ARCHITECTURE.md)** - System design and component details
- **[Development Guide](.docs/DEVELOPMENT.md)** - Contributing and development workflow

## 🎯 Example Usage

### Basic Recording
```bash
# Record frames from an IP camera
./bin/rtsp-client \
  -url rtsp://admin:password@192.168.1.100/stream \
  -output /data/camera1/frames \
  -verbose
```

### Using Makefile
```bash
# Build and run
RTSP_URL=rtsp://192.168.1.100/stream make run

# With custom arguments
RTSP_URL=rtsp://192.168.1.100/stream make run ARGS="-output /data/frames -verbose"
```

### Using Example Scripts
```bash
# Basic recording
RTSP_URL=rtsp://192.168.1.100/stream ./examples/basic.sh

# Timed session recording
RTSP_URL=rtsp://192.168.1.100/stream DURATION=120 ./examples/record_session.sh
```

## 🏗️ Architecture

```
┌─────────────────┐
│  Main App       │
└────────┬────────┘
         │
    ┌────┴────┬────────┬──────────┐
    ▼         ▼        ▼          ▼
┌────────┐ ┌──────┐ ┌────────┐ ┌─────────┐
│ Config │ │ RTSP │ │   RTP  │ │ Decoder │
└────────┘ │Client│ │ Parser │ │  H.264  │
           └──┬───┘ └────────┘ └────┬────┘
              │                      │
              │    ┌─────────────────┘
              ▼    ▼
          ┌──────────┐
          │ Storage  │
          └──────────┘
```

### Package Structure

```
rtsp-client/
├── cmd/rtsp-client/     # Main application
├── pkg/
│   ├── rtsp/           # RTSP protocol (RFC 2326)
│   ├── rtp/            # RTP packet parser (RFC 3550)
│   ├── decoder/        # H.264 decoder
│   └── storage/        # Frame storage
├── internal/config/    # Configuration
├── test/              # Integration tests
└── .docs/             # Documentation
```

## 🧪 Testing

### Run All Tests
```bash
make test
```

### Test with Coverage
```bash
make test-coverage
open coverage.html
```

### Integration Tests
```bash
# Requires real RTSP server
RTSP_URL=rtsp://192.168.1.100/stream make test-integration
```

### Test Output
```
=== RUN   TestParsePacket
--- PASS: TestParsePacket (0.00s)
=== RUN   TestH264Decoder_ProcessPacket
--- PASS: TestH264Decoder_ProcessPacket (0.00s)
...
PASS
ok      github.com/rtsp-client/pkg/rtp     0.466s
ok      github.com/rtsp-client/pkg/rtsp    0.626s
ok      github.com/rtsp-client/pkg/decoder 0.546s
ok      github.com/rtsp-client/pkg/storage 0.688s
```

## 📊 Output

Frames are saved as individual H.264 files with RTP timestamps:

```
frames/
├── 90000.h264      # Timestamp: 90000
├── 93600.h264      # Timestamp: 93600
├── 97200.h264      # Timestamp: 97200
└── ...
```

### Playing Frames

```bash
# Using FFplay
ffplay -f h264 frames/90000.h264

# Using VLC
vlc frames/90000.h264

# Convert to MP4
ffmpeg -framerate 30 -i frames/%d.h264 -c copy output.mp4
```

## 🛠️ Development

### Build
```bash
make build
```

### Format Code
```bash
make fmt
```

### Run Checks
```bash
make check  # fmt + vet + test
```

### Clean
```bash
make clean
```

## 📋 Requirements

### Minimum
- Go 1.21+
- Network access to RTSP server
- UDP ports 50000-50001 available

### Optional
- FFmpeg (for video conversion)
- golangci-lint (for linting)
- Wireshark (for debugging)

## 🤝 Contributing

Contributions are welcome! Please read [DEVELOPMENT.md](.docs/DEVELOPMENT.md) for details on our development process and coding standards.

### Development Workflow
1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Write tests first (TDD)
4. Implement the feature
5. Run tests (`make test`)
6. Commit changes (`git commit -m 'feat: add amazing feature'`)
7. Push to branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built following TDD and clean architecture principles
- Implements RTSP (RFC 2326) and RTP (RFC 3550) protocols
- H.264 NAL unit handling based on ITU-T H.264 specification

## 📞 Support

- 📖 [Documentation](.docs/)
- 🐛 [Issue Tracker](https://github.com/YOUR_USERNAME/rtsp-client/issues)
- 💬 [Discussions](https://github.com/YOUR_USERNAME/rtsp-client/discussions)

## ⭐ Star History

If you find this project useful, please consider giving it a star!

