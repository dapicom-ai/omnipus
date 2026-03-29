// Package security provides network security controls for Omnipus.
//
// This file implements SEC-24 (SSRF protection) from the Omnipus BRD:
// blocking outbound HTTP requests to private/internal IP ranges, cloud metadata
// endpoints, and providing DNS rebinding protection.
package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Private and reserved IPv4 CIDR ranges that SSRF protection blocks.
var privateIPv4Ranges = []string{
	"10.0.0.0/8",      // RFC 1918
	"172.16.0.0/12",   // RFC 1918
	"192.168.0.0/16",  // RFC 1918
	"169.254.0.0/16",  // Link-local
	"127.0.0.0/8",     // Loopback
	"0.0.0.0/8",       // Current network
	"100.64.0.0/10",   // Shared address space (CGN)
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // Documentation (TEST-NET-1)
	"198.18.0.0/15",   // Benchmarking
	"198.51.100.0/24", // Documentation (TEST-NET-2)
	"203.0.113.0/24",  // Documentation (TEST-NET-3)
}

// Private and reserved IPv6 ranges.
var privateIPv6Ranges = []string{
	"::1/128",       // Loopback
	"fe80::/10",     // Link-local
	"fc00::/7",      // Unique local
	"::ffff:0:0/96", // IPv4-mapped — checked individually against IPv4 rules
}

// SSRFChecker validates IP addresses and hostnames against SSRF rules.
type SSRFChecker struct {
	ipv4Nets  []*net.IPNet
	ipv6Nets  []*net.IPNet
	allowList map[string]bool // Allowlisted IPs (SEC-24 configurable allowlist)
	resolver  Resolver        // DNS resolver (injectable for testing)
}

// Resolver abstracts DNS resolution for testability.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// defaultResolver wraps net.Resolver.
type defaultResolver struct {
	r *net.Resolver
}

func (d *defaultResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return d.r.LookupIPAddr(ctx, host)
}

// NewSSRFChecker creates an SSRF checker with the given allowlist.
func NewSSRFChecker(allowInternal []string) *SSRFChecker {
	sc := &SSRFChecker{
		allowList: make(map[string]bool),
		resolver:  &defaultResolver{r: net.DefaultResolver},
	}

	for _, cidr := range privateIPv4Ranges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("BUG: invalid hardcoded IPv4 CIDR %q: %v", cidr, err))
		}
		sc.ipv4Nets = append(sc.ipv4Nets, ipNet)
	}

	for _, cidr := range privateIPv6Ranges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("BUG: invalid hardcoded IPv6 CIDR %q: %v", cidr, err))
		}
		sc.ipv6Nets = append(sc.ipv6Nets, ipNet)
	}

	for _, ip := range allowInternal {
		sc.allowList[ip] = true
	}

	return sc
}

// SetResolver overrides the DNS resolver (for testing).
func (sc *SSRFChecker) SetResolver(r Resolver) {
	sc.resolver = r
}

// CheckIP verifies that an IP address is not in a private/reserved range.
// Returns an error if the IP is blocked, nil if it is safe.
func (sc *SSRFChecker) CheckIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("SSRF: nil IP address rejected")
	}
	ipStr := ip.String()

	// Check allowlist first
	if sc.allowList[ipStr] {
		return nil
	}

	// Cloud metadata endpoint (exact match)
	if ipStr == "169.254.169.254" {
		return fmt.Errorf("SSRF: blocked cloud metadata endpoint %s", ipStr)
	}

	// Unwrap IPv4-mapped IPv6 addresses and check IPv4 ranges
	if ip4 := ip.To4(); ip4 != nil {
		for _, ipNet := range sc.ipv4Nets {
			if ipNet.Contains(ip4) {
				return fmt.Errorf("SSRF: blocked private IP range %s (%s)", ipStr, ipNet.String())
			}
		}
		return nil
	}

	// Pure IPv6
	for _, ipNet := range sc.ipv6Nets {
		if ipNet.Contains(ip) {
			return fmt.Errorf("SSRF: blocked private IPv6 range %s (%s)", ipStr, ipNet.String())
		}
	}

	return nil
}

// CheckHost resolves a hostname and checks all resolved IPs against SSRF rules.
// This provides DNS rebinding protection (SEC-24).
func (sc *SSRFChecker) CheckHost(ctx context.Context, host string) ([]net.IPAddr, error) {
	// If host is already an IP, check it directly
	if ip := net.ParseIP(host); ip != nil {
		if err := sc.CheckIP(ip); err != nil {
			return nil, err
		}
		return []net.IPAddr{{IP: ip}}, nil
	}

	// Resolve hostname
	addrs, err := sc.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("SSRF: DNS resolution failed for %s: %w", host, err)
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("SSRF: no addresses found for %s", host)
	}

	// Check ALL resolved IPs
	for _, addr := range addrs {
		if err := sc.CheckIP(addr.IP); err != nil {
			return nil, fmt.Errorf("SSRF: hostname %s resolved to blocked IP %s: %w", host, addr.IP, err)
		}
	}

	return addrs, nil
}

// CheckURL validates a URL string against SSRF rules.
// Extracts the host, resolves it, and checks all resolved IPs.
func (sc *SSRFChecker) CheckURL(ctx context.Context, rawURL string) error {
	host := extractHost(rawURL)
	if host == "" {
		return fmt.Errorf("SSRF: cannot extract host from URL %q", rawURL)
	}

	_, err := sc.CheckHost(ctx, host)
	return err
}

// extractHost extracts the hostname (without port) from a URL.
func extractHost(rawURL string) string {
	// Remove scheme
	url := rawURL
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	// Remove path
	if idx := strings.IndexAny(url, "/?#"); idx != -1 {
		url = url[:idx]
	}
	// Remove userinfo
	if idx := strings.LastIndex(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	// Remove port
	host, _, err := net.SplitHostPort(url)
	if err != nil {
		return url
	}
	return host
}

// SafeTransport returns an http.Transport that enforces SSRF rules on all connections.
func (sc *SSRFChecker) SafeTransport() *http.Transport {
	return &http.Transport{
		DialContext:         sc.safeDialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

func (sc *SSRFChecker) safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF: invalid address %q: %w", addr, err)
	}

	addrs, err := sc.CheckHost(ctx, host)
	if err != nil {
		return nil, err
	}

	var lastErr error
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	for _, addr := range addrs {
		target := net.JoinHostPort(addr.IP.String(), port)
		conn, err := dialer.DialContext(ctx, network, target)
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("SSRF: no connectable address for %s", host)
}

// CheckRedirect validates redirect targets against SSRF rules before following them.
func (sc *SSRFChecker) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("SSRF: too many redirects")
	}
	return sc.CheckURL(req.Context(), req.URL.String())
}

// SafeClient returns an http.Client configured with SSRF protection.
func (sc *SSRFChecker) SafeClient() *http.Client {
	return &http.Client{
		Transport:     sc.SafeTransport(),
		CheckRedirect: sc.CheckRedirect,
		Timeout:       30 * time.Second,
	}
}
