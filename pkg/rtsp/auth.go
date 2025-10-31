package rtsp

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// AuthChallenge represents authentication challenge from server
type AuthChallenge struct {
	AuthType  string // "Basic" or "Digest"
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	Qop       string // Quality of Protection
	Stale     string
}

// parseURLWithAuth parses RTSP URL and extracts authentication credentials
func parseURLWithAuth(rtspURL string) (host, port, username, password string, err error) {
	if rtspURL == "" {
		return "", "", "", "", fmt.Errorf("empty URL")
	}

	u, err := url.Parse(rtspURL)
	if err != nil {
		return "", "", "", "", err
	}

	if u.Scheme != "rtsp" {
		return "", "", "", "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "554" // Default RTSP port
	}

	// Extract credentials if present
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	return host, port, username, password, nil
}

// generateBasicAuthHeader generates Basic authentication header
func generateBasicAuthHeader(username, password string) string {
	credentials := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

// parseAuthChallenge parses WWW-Authenticate header from 401 response
func parseAuthChallenge(response string) (*AuthChallenge, error) {
	lines := strings.Split(response, "\n")

	var wwwAuth string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "WWW-Authenticate:") {
			wwwAuth = strings.TrimPrefix(line, "WWW-Authenticate:")
			wwwAuth = strings.TrimSpace(wwwAuth)
			break
		}
	}

	if wwwAuth == "" {
		return nil, fmt.Errorf("WWW-Authenticate header not found")
	}

	challenge := &AuthChallenge{}

	// Determine auth type
	if strings.HasPrefix(wwwAuth, "Basic") {
		challenge.AuthType = "Basic"
		wwwAuth = strings.TrimPrefix(wwwAuth, "Basic")
	} else if strings.HasPrefix(wwwAuth, "Digest") {
		challenge.AuthType = "Digest"
		wwwAuth = strings.TrimPrefix(wwwAuth, "Digest")
	} else {
		return nil, fmt.Errorf("unsupported authentication type")
	}

	// Parse parameters
	params := parseAuthParams(wwwAuth)
	challenge.Realm = params["realm"]
	challenge.Nonce = params["nonce"]
	challenge.Opaque = params["opaque"]
	challenge.Algorithm = params["algorithm"]
	challenge.Qop = params["qop"]
	challenge.Stale = params["stale"]

	// Default algorithm to MD5 if not specified
	if challenge.Algorithm == "" {
		challenge.Algorithm = "MD5"
	}

	return challenge, nil
}

// parseAuthParams parses authentication parameters from WWW-Authenticate header
func parseAuthParams(authStr string) map[string]string {
	params := make(map[string]string)

	// Split by comma, but be careful with quoted values
	parts := splitAuthParams(authStr)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split by first '='
		idx := strings.Index(part, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])

		// Remove quotes if present
		value = strings.Trim(value, "\"")

		params[key] = value
	}

	return params
}

// splitAuthParams splits auth parameters respecting quoted strings
func splitAuthParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '"' {
			inQuotes = !inQuotes
			current.WriteByte(ch)
		} else if ch == ',' && !inQuotes {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// generateDigestResponse generates the response hash for Digest authentication
func generateDigestResponse(username, password, realm, nonce, uri, method string) (string, error) {
	response := computeDigestResponse(username, password, realm, nonce, uri, method, "", "", "")
	return response, nil
}

// generateDigestAuthHeader generates complete Digest authentication header
func generateDigestAuthHeader(challenge *AuthChallenge, username, password, uri, method string) (string, error) {
	qop := selectQOP(challenge.Qop)
	nc := "00000001"
	cnonce := ""
	if qop != "" {
		cnonce = generateClientNonce()
	}

	response := computeDigestResponse(username, password, challenge.Realm, challenge.Nonce, uri, method, qop, nc, cnonce)

	return buildDigestHeader(challenge, username, uri, response, qop, nc, cnonce), nil
}

func computeDigestResponse(username, password, realm, nonce, uri, method, qop, nc, cnonce string) string {
	ha1 := md5Hash(username + ":" + realm + ":" + password)
	ha2 := md5Hash(method + ":" + uri)

	if qop != "" {
		return md5Hash(strings.Join([]string{ha1, nonce, nc, cnonce, qop, ha2}, ":"))
	}

	return md5Hash(strings.Join([]string{ha1, nonce, ha2}, ":"))
}

func buildDigestHeader(challenge *AuthChallenge, username, uri, response, qop, nc, cnonce string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf(`username="%s"`, username))
	parts = append(parts, fmt.Sprintf(`realm="%s"`, challenge.Realm))
	parts = append(parts, fmt.Sprintf(`nonce="%s"`, challenge.Nonce))
	parts = append(parts, fmt.Sprintf(`uri="%s"`, uri))
	parts = append(parts, fmt.Sprintf(`response="%s"`, response))

	if challenge.Opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, challenge.Opaque))
	}

	if challenge.Algorithm != "" {
		parts = append(parts, fmt.Sprintf(`algorithm=%s`, challenge.Algorithm))
	}

	if qop != "" {
		parts = append(parts, fmt.Sprintf(`qop=%s`, qop))
		parts = append(parts, fmt.Sprintf(`nc=%s`, nc))
		parts = append(parts, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}

	return "Digest " + strings.Join(parts, ", ")
}

func selectQOP(qop string) string {
	if qop == "" {
		return ""
	}

	parts := strings.Split(qop, ",")
	for _, part := range parts {
		value := strings.TrimSpace(strings.ToLower(part))
		if value == "auth" {
			return "auth"
		}
	}

	first := strings.TrimSpace(parts[0])
	return strings.ToLower(first)
}

func generateClientNonce() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// md5Hash computes MD5 hash and returns hex string
func md5Hash(data string) string {
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
}
