package helper

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// privateIPRanges contains all RFC-defined private and reserved IP ranges
var privateIPRanges []*net.IPNet

func init() {
	cidrs := []string{
		"0.0.0.0/8",       // "This" network
		"10.0.0.0/8",      // RFC 1918
		"100.64.0.0/10",   // Shared address space
		"127.0.0.0/8",     // Loopback
		"169.254.0.0/16",  // Link-local / cloud metadata
		"172.16.0.0/12",   // RFC 1918
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // Documentation (TEST-NET-1)
		"192.168.0.0/16",  // RFC 1918
		"198.18.0.0/15",   // Benchmark testing
		"198.51.100.0/24", // Documentation (TEST-NET-2)
		"203.0.113.0/24",  // Documentation (TEST-NET-3)
		"224.0.0.0/4",     // Multicast
		"240.0.0.0/4",     // Reserved
		"::1/128",         // IPv6 loopback
		"fc00::/7",        // IPv6 unique local
		"fe80::/10",       // IPv6 link-local
		"::ffff:0:0/96",   // IPv4-mapped IPv6
	}
	for _, cidr := range cidrs {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil {
			privateIPRanges = append(privateIPRanges, network)
		}
	}
}

// isPrivateIP checks if an IP address falls within any private or reserved range
func isPrivateIP(ip net.IP) bool {
	for _, network := range privateIPRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateExternalURL validates that a URL is safe to fetch (no SSRF)
func ValidateExternalURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %v", err)
	}

	// Enforce http or https scheme only
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got: %s", scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Resolve hostname to IP addresses
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %q: %v", host, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("hostname %q resolved to no addresses", host)
	}

	// Check all resolved IPs against private ranges
	for _, ipAddr := range ips {
		if isPrivateIP(ipAddr.IP) {
			return fmt.Errorf("URL resolves to a private/reserved IP address")
		}
	}

	return nil
}

// SSRFSafeDialContext returns a dial function that blocks connections to private IPs.
// Use this with http.Transport to prevent DNS rebinding attacks.
func SSRFSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %v", err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %q: %v", host, err)
	}

	for _, ipAddr := range ips {
		if isPrivateIP(ipAddr.IP) {
			return nil, fmt.Errorf("connection to private/reserved IP address blocked")
		}
	}

	// Connect to the first valid IP
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}
