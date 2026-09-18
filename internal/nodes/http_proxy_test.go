package nodes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPChecksIgnoreEnvironmentProxy(t *testing.T) {
	const childScheme = "PATCHBAY_HTTP_PROXY_TEST_SCHEME"
	scheme := os.Getenv(childScheme)
	if scheme == "" {
		// Go caches proxy environment settings. A separate process per protocol
		// prevents other HTTP tests or the user's environment from masking this bug.
		for _, scheme := range []string{"http", "https"} {
			t.Run(scheme, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPChecksIgnoreEnvironmentProxy$")
				for _, entry := range os.Environ() {
					key, _, _ := strings.Cut(entry, "=")
					switch strings.ToUpper(key) {
					case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", childScheme:
						continue
					}
					cmd.Env = append(cmd.Env, entry)
				}
				cmd.Env = append(cmd.Env, childScheme+"="+scheme)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s proxy regression: %v\n%s", scheme, err, output)
				}
			})
		}
		return
	}
	if scheme != "http" && scheme != "https" {
		t.Fatalf("unexpected child protocol %q", scheme)
	}

	// Loopback URLs bypass Go's environment proxy automatically. Use a name
	// covered by httptest's TLS certificate, but route every dial locally below.
	const endpointHost = "check.example.com"
	var endpointRequests, proxyRequests atomic.Int32
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpointRequests.Add(1)
		if r.Host != endpointHost || r.URL.Path != "/health" || r.Method != http.MethodGet {
			t.Errorf("unexpected endpoint request: %s %s host=%s", r.Method, r.URL, r.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	if scheme == "https" {
		endpoint.StartTLS()
	} else {
		endpoint.Start()
	}
	defer endpoint.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		// Reject both HTTP forwarding and HTTPS CONNECT; never contact a target.
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)

	node := NewHTTP()
	defer node.client.CloseIdleConnections()
	step := checkStep(scheme + "://" + endpointHost + "/health")
	step.Config.TimeoutMS = 2000
	request, err := http.NewRequest(http.MethodGet, step.Config.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This also proves the fixture would use the proxy with Go's defaults and
	// catches changes that disable proxies globally instead of on the clone.
	defaultTransport := http.DefaultTransport.(*http.Transport)
	if defaultTransport.Proxy == nil {
		t.Fatal("NewHTTP changed the shared default transport's proxy policy")
	}
	configuredProxy, err := defaultTransport.Proxy(request)
	if err != nil || configuredProxy == nil || configuredProxy.String() != proxy.URL {
		t.Fatalf("fixture must enable the environment proxy: proxy=%v error=%v", configuredProxy, err)
	}
	transport := node.client.Transport.(*http.Transport)
	if scheme == "https" {
		roots := x509.NewCertPool()
		roots.AddCert(endpoint.Certificate())
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	}
	port := "80"
	if scheme == "https" {
		port = "443"
	}
	endpointAddress := net.JoinHostPort(endpointHost, port)
	proxyAddress := proxy.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		var localAddress string
		switch address {
		case endpointAddress:
			localAddress = endpoint.Listener.Addr().String()
		case proxyAddress:
			localAddress = proxyAddress
		default:
			return nil, fmt.Errorf("unexpected dial address %q", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, localAddress)
	}
	// Keep the transport's Proxy untouched: this is the production policy under test.
	result, err := node.Execute(context.Background(), step)
	if err != nil || !result.Healthy || result.StatusCode != http.StatusOK || endpointRequests.Load() != 1 || proxyRequests.Load() != 0 {
		t.Fatalf("check did not reach the endpoint directly: result=%+v error=%v endpoint requests=%d proxy requests=%d",
			result, err, endpointRequests.Load(), proxyRequests.Load())
	}
}
