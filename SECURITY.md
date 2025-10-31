# Security Policy

## Supported Versions

We actively support the following versions with security updates:

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |
| < Latest| :x:                |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security vulnerability, please follow these steps:

1. **Do NOT** create a public GitHub issue for security vulnerabilities
2. Email security details to the maintainers (include in your repository description or README)
3. Include the following information:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if available)

### Response Timeline

- **Initial Response**: Within 48 hours
- **Status Update**: Within 7 days
- **Fix Timeline**: Depends on severity and complexity

### What to Report

- Authentication bypass
- Remote code execution
- Denial of service vulnerabilities
- Data leakage or unauthorized access
- Protocol-level security issues (RTSP/RTP)
- Buffer overflows or memory safety issues

### What NOT to Report

- Issues requiring physical access to the device
- Issues in dependencies (report to those projects directly)
- Denial of service from resource exhaustion
- Missing security best practices that don't directly cause vulnerabilities

## Security Best Practices

When using this RTSP client:

1. **Network Security**: Use VPN or secure networks when connecting to RTSP streams
2. **Authentication**: Always use strong credentials for RTSP authentication
3. **TLS/Encryption**: Consider using RTSP over TLS (RTSPS) when available
4. **Input Validation**: Validate all RTSP URLs and credentials before use
5. **Dependencies**: Keep dependencies up to date (`go mod tidy`)

## Acknowledgments

We appreciate responsible disclosure of security vulnerabilities. Contributors who report valid security issues will be credited in our security acknowledgments (unless they prefer to remain anonymous).

