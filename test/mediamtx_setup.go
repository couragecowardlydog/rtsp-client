package test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// MediaMTXServer manages a MediaMTX Docker container for testing
type MediaMTXServer struct {
	containerName string
	port          int
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
}

// NewMediaMTXServer creates a new MediaMTX server instance
func NewMediaMTXServer(containerName string, port int) *MediaMTXServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &MediaMTXServer{
		containerName: containerName,
		port:          port,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start starts the MediaMTX Docker container
func (m *MediaMTXServer) Start() error {
	if m.isRunning {
		return fmt.Errorf("MediaMTX server is already running")
	}

	// Check if Docker is available
	if err := m.checkDocker(); err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}

	// Stop any existing container with the same name
	m.stopExistingContainer()

	// Start the container
	cmd := exec.CommandContext(
		m.ctx,
		"docker", "run",
		"-d", // detached mode
		"-p", fmt.Sprintf("%d:8554", m.port), // port mapping
		"--name", m.containerName,
		"--rm", // auto-remove on stop
		"bluenviron/mediamtx",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a port conflict
		outputStr := string(output)
		if contains(outputStr, "port is already allocated") || contains(outputStr, "address already in use") {
			return fmt.Errorf("failed to start MediaMTX container: port %d is already in use. Stop existing containers first: %w", m.port, err)
		}
		return fmt.Errorf("failed to start MediaMTX container: %w\nOutput: %s", err, outputStr)
	}

	m.isRunning = true

	// Wait for container to be ready
	return m.waitForReady(10 * time.Second)
}

// Stop stops the MediaMTX Docker container
func (m *MediaMTXServer) Stop() error {
	if !m.isRunning {
		return nil
	}

	// Cancel context to stop any running processes
	m.cancel()

	// Stop the container
	cmd := exec.Command("docker", "stop", m.containerName)
	cmd.Run() // Ignore errors as container might already be stopped

	m.isRunning = false
	return nil
}

// URL returns the RTSP URL for a stream
func (m *MediaMTXServer) URL(streamPath string) string {
	if streamPath[0] != '/' {
		streamPath = "/" + streamPath
	}
	return fmt.Sprintf("rtsp://127.0.0.1:%d%s", m.port, streamPath)
}

// IsRunning returns whether the server is running
func (m *MediaMTXServer) IsRunning() bool {
	return m.isRunning
}

// Port returns the server port
func (m *MediaMTXServer) Port() int {
	return m.port
}

// checkDocker verifies Docker is available
func (m *MediaMTXServer) checkDocker() error {
	cmd := exec.Command("docker", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker command not found: %w", err)
	}
	return nil
}

// stopExistingContainer stops any existing container with the same name or using the same port
func (m *MediaMTXServer) stopExistingContainer() {
	// Stop container with the same name
	cmd := exec.Command("docker", "stop", m.containerName)
	cmd.Run() // Ignore errors

	cmd = exec.Command("docker", "rm", m.containerName)
	cmd.Run() // Ignore errors

	// Also check for containers using the same port
	// List all running containers and check their port mappings
	cmd = exec.Command("docker", "ps", "--format", "{{.Names}} {{.Ports}}")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if strings.Contains(line, fmt.Sprintf(":%d->", m.port)) || strings.Contains(line, fmt.Sprintf("0.0.0.0:%d->", m.port)) {
				// Extract container name (first field)
				parts := strings.Fields(line)
				if len(parts) > 0 {
					name := parts[0]
					if name != m.containerName {
						// Stop container using the same port
						stopCmd := exec.Command("docker", "stop", name)
						stopCmd.Run() // Ignore errors
					}
				}
			}
		}
	}
}

// waitForReady waits for the container to be ready by checking if it's running
func (m *MediaMTXServer) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", m.containerName), "--format", "{{.Names}}")
		output, err := cmd.Output()
		if err == nil && strings.Contains(string(output), m.containerName) {
			// Container is running, give it a moment to fully initialize
			time.Sleep(1 * time.Second)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("MediaMTX container failed to start within %v", timeout)
}



