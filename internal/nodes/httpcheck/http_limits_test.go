package httpcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"patchbay/internal/nodes/resulttext"
)

func TestHTTPResponseLimits(t *testing.T) {
	for _, test := range []struct {
		name, response, wantReason string
		healthy, truncated         bool
	}{
		{"valid headers", "HTTP/1.1 200 OK\r\nX-Padding: " + strings.Repeat("a", maxHTTPHeaderBytes/2) + "\r\nContent-Length: 0\r\n\r\n", "Received expected HTTP 200", true, false},
		{"malformed status", strings.Repeat("x", 3*resulttext.MaxReasonBytes) + "\r\n\r\n", "malformed HTTP response", false, true},
		{"malformed header", "HTTP/1.1 200 OK\r\n" + strings.Repeat("x", 3*resulttext.MaxReasonBytes) + "\r\n\r\n", "malformed MIME header", false, true},
		{"oversized status", strings.Repeat("x", 2*maxHTTPHeaderBytes) + "\r\n\r\n", "response headers exceeded", false, false},
		{"oversized headers", "HTTP/1.1 200 OK\r\nX-Padding: " + strings.Repeat("a", 2*maxHTTPHeaderBytes) + "\r\nContent-Length: 0\r\n\r\n", "response headers exceeded", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Hijack only this local fixture to send bytes net/http would not emit.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Error(err)
					return
				}
				// Early connection closure is expected when the client rejects headers.
				_, _ = io.WriteString(conn, test.response)
			}))
			defer server.Close()
			node := New()
			defer node.client.CloseIdleConnections()
			step := checkStep(server.URL)
			step.Config.TimeoutMS = 2000
			result, err := node.Execute(context.Background(), step)
			if err != nil || result.Healthy != test.healthy || !strings.Contains(result.Reason, test.wantReason) {
				t.Fatalf("unexpected response classification: healthy=%v status=%d reason=%.200q error=%v", result.Healthy, result.StatusCode, result.Reason, err)
			}
			if (test.healthy && result.StatusCode != 200) || (!test.healthy && result.StatusCode != 0) {
				t.Fatalf("invalid response status: %d", result.StatusCode)
			}
			if len(result.Reason) > resulttext.MaxReasonBytes || !utf8.ValidString(result.Reason) || strings.HasSuffix(result.Reason, "... [truncated]") != test.truncated {
				t.Fatalf("response reason was not safely bounded: length=%d", len(result.Reason))
			}
		})
	}
}

func TestHTTP2ResponseHeaderLimit(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		name := "valid headers"
		if oversized {
			name = "oversized headers"
		}
		t.Run(name, func(t *testing.T) {
			protocol := make(chan int, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case protocol <- r.ProtoMajor:
				default:
				}
				size := maxHTTPHeaderBytes / 2
				if oversized {
					size = 2 * maxHTTPHeaderBytes
				}
				// Test the total decoded size, not one oversized HPACK string.
				for range size / 1024 {
					w.Header().Add("X-Padding", strings.Repeat("a", 1024))
				}
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(http.StatusOK)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			node := New()
			defer node.client.CloseIdleConnections()
			// Trust this fixture's certificate without disabling TLS verification.
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			node.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
			step := checkStep(server.URL)
			step.Config.TimeoutMS = 2000
			result, err := node.Execute(context.Background(), step)
			wantHealthy := !oversized
			if err != nil || result.Healthy != wantHealthy || len(result.Reason) > resulttext.MaxReasonBytes || !utf8.ValidString(result.Reason) {
				t.Fatalf("HTTP/2 header limit was not enforced: %+v, %v", result, err)
			}
			if oversized && (result.StatusCode != 0 || !strings.Contains(result.Reason, "header list")) {
				t.Fatalf("oversized HTTP/2 headers did not produce a request-failure result: %+v", result)
			}
			select {
			case got := <-protocol:
				if got != 2 {
					t.Fatalf("fixture used HTTP/%d instead of HTTP/2", got)
				}
			default:
				t.Fatal("request did not reach the HTTP/2 fixture")
			}
		})
	}
}
