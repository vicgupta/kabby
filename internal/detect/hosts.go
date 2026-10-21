package detect

import (
	"fmt"
	"net/url"
	"strings"
)

// SavedHost is a remote machine saved in ~/.kabby.json.
type SavedHost struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// ProbeURLs returns the URLs to probe for this host.
// A bare host tries both usual ports (11434, 1234).
// host:port or a full URL names a single server.
func (h SavedHost) ProbeURLs() []string {
	if strings.Contains(h.Address, "://") {
		// Full URL — use as-is.
		return []string{h.Address}
	}
	addr := h.Address
	if _, _, err := netHostPort(addr); err == nil {
		// host:port form — single URL.
		return []string{"http://" + addr}
	}
	// Bare host — try both usual ports.
	return []string{
		"http://" + addr + ":11434",
		"http://" + addr + ":1234",
	}
}

func netHostPort(addr string) (string, int, error) {
	host, portStr, err := netSplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return "", 0, fmt.Errorf("not a port")
		}
		port = port*10 + int(c-'0')
	}
	return host, port, nil
}

func netSplitHostPort(addr string) (string, string, error) {
	// If it contains a colon, try host:port split.
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		host := addr[:idx]
		port := addr[idx+1:]
		if host != "" && port != "" {
			return host, port, nil
		}
	}
	return "", "", fmt.Errorf("no port")
}

// Validate checks that the host address is well-formed.
func (h SavedHost) Validate() error {
	if h.Address == "" {
		return fmt.Errorf("host address is empty")
	}
	if strings.Contains(h.Address, "://") {
		if _, err := url.Parse(h.Address); err != nil {
			return fmt.Errorf("invalid host URL: %v", err)
		}
		return nil
	}
	// Check for host:port or bare hostname.
	parts := strings.Split(h.Address, ":")
	if len(parts) > 2 {
		return fmt.Errorf("invalid host address: %s", h.Address)
	}
	return nil
}
