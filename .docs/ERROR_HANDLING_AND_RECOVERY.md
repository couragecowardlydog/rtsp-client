# 🔄 Error Handling & Recovery

## Overview

The RTSP client is designed to **never crash** when cameras go offline or network issues occur. Instead, it gracefully handles errors, logs meaningful messages, and automatically attempts recovery.

## 🎯 What Problems Does This Solve?

✅ Camera goes offline → **Auto-reconnect**  
✅ Network temporarily down → **Retry with backoff**  
✅ Camera reboots → **Re-establish connection**  
✅ Session expires → **Recover or recreate**  
✅ Intermittent packet loss → **Continue streaming**  

## 🏗️ How It Works

### 1. Initial Connection with Retry

When you start the client, it doesn't just try once and fail. It uses **exponential backoff retry**:

```go
// Configure retry behavior (5 attempts, 1s to 30s backoff)
retryConfig := rtsp.NewRetryConfig(5, 1*time.Second, 30*time.Second)
client.SetRetryConfig(retryConfig)

// This will retry automatically if initial connection fails
if err := client.ConnectWithRetry(); err != nil {
    return fmt.Errorf("connection failed: %w", err)
}
```

**Retry Schedule:**
- Attempt 1: Immediate
- Attempt 2: Wait 1 second
- Attempt 3: Wait 2 seconds
- Attempt 4: Wait 4 seconds
- Attempt 5: Wait 8 seconds

### 2. Connection Health Monitoring

The client tracks connection health in real-time:

```go
consecutiveErrors := 0
maxConsecutiveErrors := 10  // Recovery threshold

// After 10 consecutive packet read errors → trigger recovery
if consecutiveErrors >= maxConsecutiveErrors {
    log.Printf("🔄 Connection lost. Attempting to recover...")
    recoverConnection()
}
```

### 3. Smart Recovery Strategy

When connection issues are detected, the client tries two recovery methods:

#### Method A: Session Recovery (Fast) ⚡

If the RTSP session might still be valid on the server:

```go
// Try to resume with existing session
err := client.RecoverSession()
```

This is **faster** because:
- Doesn't need to re-do DESCRIBE
- Doesn't need to re-negotiate ports
- Just reconnects TCP and resumes PLAY

#### Method B: Full Re-establishment (Thorough) 🔧

If session recovery fails, completely re-establish:

```go
// Close everything
client.Close()

// Re-do the full RTSP handshake
Connect() → Describe() → Setup() → Play()
```

### 4. Decoder State Reset

**Important:** When reconnecting, the H.264 decoder state is reset:

```go
decoder.Reset()  // Clear any partial frame buffers
```

This prevents corrupted frames from mixing old and new stream data.

## 📊 Recovery Metrics

The client tracks recovery statistics:

```go
metrics := client.GetRecoveryMetrics()

// Available metrics:
- TotalRetries          // How many recovery attempts
- SuccessfulRecoveries  // How many succeeded
- FailedRecoveries      // How many failed
- LastRecoveryAttempt   // Timestamp of last attempt
- LastRecoverySuccess   // Timestamp of last success
```

Stats are logged every 5 seconds:
```
📊 Stats - Frames: 1523 (52 keyframes) | Recoveries: 3 successful, 0 failed
```

## 🎮 Example Scenarios

### Scenario 1: Camera Reboots

```
[15:30:45] 📦 Receiving packets normally...
[15:30:50] ⚠️  Error reading packet: connection refused (consecutive errors: 1)
[15:30:51] ⚠️  Error reading packet: connection refused (consecutive errors: 2)
...
[15:30:55] 🔄 Connection lost. Attempting to recover...
[15:30:55] 🔄 Attempting session recovery...
[15:30:55] Session recovery failed: connection refused. Re-establishing connection...
[15:30:56] 🔌 Connecting to RTSP server...
[15:31:00] ✅ Full connection re-established
[15:31:00] ✅ Connection recovered successfully!
[15:31:01] 📦 Receiving packets normally...
```

### Scenario 2: Network Hiccup (Session Still Valid)

```
[16:15:30] 📦 Receiving packets normally...
[16:15:35] ⚠️  Error reading packet: i/o timeout (consecutive errors: 10)
[16:15:35] 🔄 Connection lost. Attempting to recover...
[16:15:35] 🔄 Attempting session recovery...
[16:15:36] ✅ Session recovered successfully!
[16:15:36] ✅ Connection recovered successfully!
[16:15:37] 📦 Receiving packets normally...
```

### Scenario 3: Intermittent Packet Loss

```
[17:00:00] 📦 Receiving packets normally...
[17:00:05] ⚠️  Error reading packet: connection reset (consecutive errors: 1)
[17:00:05] 📦 Packet received (consecutive errors reset after 5 successful reads)
[17:00:06] ✅ Connection stable. Resetting error count.
```

## ⚙️ Configuration Options

### Adjust Recovery Sensitivity

```go
// More aggressive recovery (after 5 errors)
const maxConsecutiveErrors = 5

// More patient recovery (after 20 errors)
const maxConsecutiveErrors = 20
```

### Adjust Retry Behavior

```go
// Quick retries (good for local cameras)
retryConfig := rtsp.NewRetryConfig(
    3,                    // Max 3 attempts
    500*time.Millisecond, // Start with 500ms
    5*time.Second,        // Max 5s between retries
)

// Patient retries (good for remote/cloud cameras)
retryConfig := rtsp.NewRetryConfig(
    10,                  // Max 10 attempts
    2*time.Second,       // Start with 2s
    60*time.Second,      // Max 1 minute between retries
)
```

### Custom Retry Logic

You can also manually control retry:

```go
// Manual retry with custom logic
for attempts := 0; attempts < 5; attempts++ {
    if err := client.Connect(); err == nil {
        break
    }
    
    log.Printf("Attempt %d failed, waiting...", attempts+1)
    time.Sleep(time.Duration(attempts+1) * 2 * time.Second)
}
```

## 🚨 Error Types Handled

### Network Errors
- `connection refused` → Camera offline
- `connection reset` → Network interruption
- `i/o timeout` → Slow network or packet loss
- `no route to host` → Network/routing issue

### RTSP Protocol Errors
- `454 Session Not Found` → Session expired
- `503 Service Unavailable` → Camera overloaded
- `500 Internal Server Error` → Camera software issue

### Application Errors
- Packet parse errors → Bad data received
- Frame save errors → Disk full or I/O issue (non-fatal)

## 🔧 Troubleshooting

### Issue: Client keeps failing to recover

**Check:**
1. Is the camera actually back online?
2. Did the camera IP address change?
3. Are credentials still valid?
4. Is the network stable?

**Solution:**
- Increase max retry attempts
- Increase delay between retries
- Check camera logs
- Test with `ping` and `telnet`

### Issue: Too many recovery attempts

**Reason:** Network is very unstable

**Solution:**
```go
// Increase error threshold
const maxConsecutiveErrors = 20  // More patient

// Longer backoff
retryConfig := rtsp.NewRetryConfig(10, 5*time.Second, 2*time.Minute)
```

### Issue: Connection keeps dropping

**Possible causes:**
- Camera has connection timeout
- Firewall dropping connections
- No keep-alive messages

**Solution:**
The client has built-in keep-alive support (see `pkg/rtsp/keepalive.go`)

## 📝 Best Practices

### 1. Always Use Retry on Initial Connect
```go
// ❌ Bad: Single attempt
client.Connect()

// ✅ Good: Retry with backoff
client.ConnectWithRetry()
```

### 2. Monitor Recovery Metrics
```go
// Log metrics periodically
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        metrics := client.GetRecoveryMetrics()
        if metrics.FailedRecoveries > 10 {
            alert("Too many failed recoveries!")
        }
    }
}()
```

### 3. Reset Decoder on Recovery
```go
// ✅ Always reset decoder when reconnecting
decoder.Reset()
```

This prevents frame corruption from mixing old and new data.

### 4. Handle Context Cancellation
```go
// Check context before long operations
if ctx.Err() != nil {
    return ctx.Err()  // Don't retry if shutting down
}
```

## 🎓 Understanding Exponential Backoff

Why not just retry immediately forever?

❌ **Immediate retry:**
```
Retry 1: Now
Retry 2: Now
Retry 3: Now
→ Floods the network, wastes resources
```

✅ **Exponential backoff:**
```
Retry 1: Now
Retry 2: Wait 1s
Retry 3: Wait 2s
Retry 4: Wait 4s
→ Gives network/camera time to recover
```

## 💡 Key Takeaways

1. **The client never crashes from network issues** - it logs and retries
2. **Recovery is automatic** - no manual intervention needed
3. **Two-phase recovery** - fast session resume, then full reconnect
4. **Decoder state is managed** - prevents frame corruption
5. **Metrics are tracked** - monitor health over time
6. **Exponential backoff** - gentle on network and camera
7. **Context-aware** - respects graceful shutdown

## 📁 Related Files

- `cmd/rtsp-client/main.go` - Main application with recovery loop
- `pkg/rtsp/recovery.go` - Recovery and retry logic
- `pkg/rtsp/errors.go` - Error types and handling
- `pkg/rtsp/keepalive.go` - Keep-alive to prevent timeouts

## 🔗 See Also

- [Architecture Documentation](.docs/ARCHITECTURE.md)
- [RTP Packet to Frame Conversion](.docs/RTP_PACKET_TO_FRAME.md)

