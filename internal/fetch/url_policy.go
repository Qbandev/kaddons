package fetch

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidatePublicHTTPSURL blocks unsafe URL targets to reduce SSRF risk.
func ValidatePublicHTTPSURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", rawURL)
	}

	hostname := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	if hostname == "" {
		return fmt.Errorf("URL must include a hostname: %s", rawURL)
	}

	if isBlockedHostname(hostname) {
		return fmt.Errorf("blocked URL host: %s", hostname)
	}

	if ip := net.ParseIP(hostname); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("blocked URL IP: %s", ip.String())
	}

	return nil
}

func isBlockedHostname(hostname string) bool {
	if hostname == "localhost" || strings.HasSuffix(hostname, ".local") {
		return true
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsPrivate() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}
