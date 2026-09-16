package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// HostGuard restricts the hostnames served by the application. It does not
// authenticate callers. Its allowlist is fixed at startup and safe for concurrent reads.
type HostGuard struct {
	allowed map[string]bool
}

// NewHostGuard allows loopback hosts plus comma-separated exact hostnames/IPs.
// Additional entries have no scheme, port, or wildcard; IPv6 entries are unbracketed.
func NewHostGuard(additionalHosts string) (*HostGuard, error) {
	guard := &HostGuard{allowed: map[string]bool{
		"localhost": true, "127.0.0.1": true, "::1": true,
	}}
	if strings.TrimSpace(additionalHosts) == "" {
		return guard, nil
	}
	for _, entry := range strings.Split(additionalHosts, ",") {
		host, ok := normalizeHostname(strings.TrimSpace(entry))
		if !ok {
			return nil, fmt.Errorf("invalid allowed host %q: use an exact hostname or specific IP without a scheme, port, wildcard, or IPv6 brackets", entry)
		}
		guard.allowed[host] = true
	}
	return guard, nil
}

// Wrap checks the request's Host before any API or frontend handler runs.
// Forwarded headers and DNS lookups must not override the requested hostname.
func (guard *HostGuard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, ok := requestHostname(r.Host)
		if !ok || !guard.allowed[host] {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusForbidden, "request host is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestHostname(authority string) (string, bool) {
	host, port, err := net.SplitHostPort(authority)
	if err == nil {
		// Ports do not affect the allowlist, but a supplied port must be valid.
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", false
		}
	} else {
		host = authority
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		} else if strings.ContainsAny(host, ":[]") {
			return "", false
		}
	}
	if strings.HasPrefix(authority, "[") {
		address, err := netip.ParseAddr(host)
		if err != nil || !address.Is6() {
			return "", false
		}
	}
	return normalizeHostname(host)
}

func normalizeHostname(host string) (string, bool) {
	if !strings.Contains(host, ":") {
		host = strings.TrimSuffix(host, ".")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return "", false
		}
		address = address.Unmap()
		if address.IsUnspecified() {
			return "", false
		}
		return address.String(), true
	}
	host = strings.ToLower(host)
	if host == "" || len(host) > 253 {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", false
			}
		}
	}
	return host, true
}
