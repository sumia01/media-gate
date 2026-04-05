package safenet

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// IsPrivateIP reports whether ip is in a private, loopback, or link-local range.
func IsPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// SafeTransport returns an *http.Transport that rejects connections to private IPs.
// Hostnames are resolved first; if any resolved address is private the dial is refused.
func SafeTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("safenet: invalid address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("safenet: DNS lookup for %q failed: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("safenet: no addresses found for %q", host)
			}
			for _, ip := range ips {
				if IsPrivateIP(ip.IP) {
					return nil, fmt.Errorf("safenet: connections to private address %s are not allowed", ip.IP)
				}
			}
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
}

// SafeClient returns an *http.Client that rejects connections to private IPs.
func SafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: SafeTransport(),
	}
}

// ValidateURLScheme checks that rawURL parses successfully and has an http or https scheme.
func ValidateURLScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}
	return nil
}

// CheckHost resolves the hostname from rawURL and rejects private IPs.
func CheckHost(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must have a host")
	}

	// Try parsing as literal IP first.
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("connections to private address %s are not allowed", ip)
		}
		return nil
	}

	// Resolve hostname.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("DNS lookup for %q failed: %w", host, err)
	}
	for _, ip := range ips {
		if IsPrivateIP(ip.IP) {
			return fmt.Errorf("connections to private address %s (%s) are not allowed", ip.IP, host)
		}
	}
	return nil
}
