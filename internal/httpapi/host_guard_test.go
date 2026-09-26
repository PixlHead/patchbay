package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestHostGuard(t *testing.T) {
	for _, test := range []struct {
		name, host, additional string
		allowed                bool
	}{
		{"localhost without port", "localhost", "", true},
		{"Docker browser access", "localhost:8080", "", true},
		{"Docker health check", "127.0.0.1:8080", "", true},
		{"Vite proxy preserves browser host", "127.0.0.1:5173", "", true},
		{"Playwright server", "127.0.0.1:18080", "", true},
		{"case and DNS trailing dot", "LOCALHOST.:8080", "", true},
		{"IPv6 without port", "[::1]", "", true},
		{"IPv6 with port", "[::1]:8080", "", true},
		{"expanded IPv6", "[0:0:0:0:0:0:0:1]:8080", "", true},
		{"mapped IPv4", "[::ffff:127.0.0.1]:8080", "", true},
		{"configured hostname", "patchbay.home.example:8080", " Patchbay.Home.Example. ,192.0.2.10", true},
		{"configured IPv4", "192.0.2.10:8080", "patchbay.home.example,192.0.2.10", true},
		{"configured IPv6", "[2001:db8::10]:8080", "2001:db8::10", true},
		{"additional hosts preserve defaults", "localhost:8080", "patchbay.home.example", true},
		{"empty configuration", "localhost:8080", "  ", true},
		{"foreign hostname", "attacker.example:8080", "", false},
		{"localhost suffix attack", "localhost.attacker.example:8080", "", false},
		{"localhost subdomain", "attacker.localhost:8080", "", false},
		{"configured host suffix attack", "patchbay.home.example.attacker.example", "patchbay.home.example", false},
		{"unconfigured hostname", "patchbay.home.example:8080", "", false},
		{"unconfigured IP", "192.0.2.10:8080", "", false},
		{"wildcard IPv4 bind address", "0.0.0.0:8080", "", false},
		{"wildcard IPv4 with trailing dot", "0.0.0.0.:8080", "", false},
		{"wildcard IPv6 bind address", "[::]:8080", "", false},
		{"missing host", "", "", false},
		{"empty port", "localhost:", "", false},
		{"nonnumeric port", "localhost:http", "", false},
		{"out of range port", "localhost:65536", "", false},
		{"zero port", "localhost:0", "", false},
		{"multiple ports", "localhost:80:90", "", false},
		{"userinfo", "attacker@localhost:8080", "", false},
		{"URL instead of host", "http://localhost:8080", "", false},
		{"path", "localhost/path", "", false},
		{"whitespace", " localhost:8080", "", false},
		{"duplicate host values", "localhost,attacker.example", "", false},
		{"invalid DNS label", "localhost..", "", false},
		{"bracketed hostname", "[localhost]:8080", "", false},
		{"unbracketed IPv6", "::1", "", false},
		{"IPv6 zone", "[::1%lo0]:8080", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			guard, err := NewHostGuard(test.additional)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			handler := guard.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			request.Host = test.host
			// These headers must never rescue a rejected request.Host.
			request.Header.Set("Host", "localhost:8080")
			request.Header.Set("X-Forwarded-Host", "localhost:8080")
			request.Header.Set("Forwarded", "host=localhost:8080")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := http.StatusForbidden
			if test.allowed {
				want = http.StatusNoContent
			}
			if response.Code != want || called != test.allowed {
				t.Fatalf("host %q: status=%d handler called=%v; want status=%d called=%v", test.host, response.Code, called, want, test.allowed)
			}
			if !test.allowed && (response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store") {
				t.Fatalf("unexpected rejection headers: %v", response.Header())
			}
		})
	}
}

func TestHostGuardRejectsInvalidConfiguration(t *testing.T) {
	for _, value := range []string{
		"*", "*.example", ".example", "https://patchbay.example", "patchbay.example:8080",
		"[::1]", "localhost,", ",localhost", "localhost,,127.0.0.1", "user@localhost",
		"localhost/path", "local host", "bad..example", "-bad.example", "bad-.example",
		"0.0.0.0", "0.0.0.0.", "::", "::1.", "::ffff:0.0.0.0", "fe80::1%en0", "::ffff:127.0.0.1%en0",
		strings.Repeat("a", 64) + ".example",
	} {
		t.Run(value, func(t *testing.T) {
			guard, err := NewHostGuard(value)
			if err == nil || guard != nil {
				t.Fatalf("invalid host configuration %q was accepted", value)
			}
		})
	}
}

func TestHostGuardProtectsAPIAndFrontend(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	runner, err := engine.New(2, 0, func(context.Context, workflow.Step) (workflow.CheckResult, error) {
		return workflow.CheckResult{Healthy: true}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("private frontend"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := NewHostGuard("")
	if err != nil {
		t.Fatal(err)
	}
	handler := guard.Wrap(New(testDefinitions(), runner, nil, db, webDir))
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/health"},
		{http.MethodGet, "/api/workflows"},
		{http.MethodGet, "/api/runs"},
		{http.MethodGet, "/api/runs/missing"},
		{http.MethodPost, "/api/workflows/test-workflow/runs"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Host = "attacker.example:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || strings.TrimSpace(response.Body.String()) != `{"error":"request host is not allowed"}` {
			t.Fatalf("%s %s was not blocked: %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if len(runner.List()) != 0 {
		t.Fatal("a rejected host started a workflow")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("normal health check failed: %d %s", response.Code, response.Body.String())
	}
}
