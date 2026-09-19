// Package httpcheck executes HTTP health checks.
package httpcheck

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"patchbay/internal/nodes/resulttext"
	"patchbay/internal/workflow"
)

const maxHTTPHeaderBytes = 64 * 1024

// Executor owns a reusable client and supports concurrent checks.
type Executor struct {
	client *http.Client
}

// New creates an HTTP executor with direct connections and bounded headers.
func New() *Executor {
	// Keep Go's connection, TLS, and HTTP/2 defaults without changing
	// the shared transport used by other clients.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Health checks connect directly to their targets, regardless of the
	// server's HTTP_PROXY, HTTPS_PROXY, or NO_PROXY environment settings.
	transport.Proxy = nil
	transport.MaxResponseHeaderBytes = maxHTTPHeaderBytes
	return &Executor{client: &http.Client{
		Transport: transport,
		// Observe the configured endpoint's response, including redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *Executor) Execute(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
	config := step.Config
	result := workflow.CheckResult{URL: config.URL, ExpectedStatus: config.ExpectedStatus}
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, config.URL, nil)
	if err != nil {
		return result, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("User-Agent", "patchbay-m0")
	start := time.Now()
	response, err := h.client.Do(req)
	result.DurationMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		if response != nil {
			response.Body.Close()
		}
		return result, ctx.Err()
	}
	if err != nil {
		if checkCtx.Err() != nil {
			result.Reason = fmt.Sprintf("No response within %d ms", config.TimeoutMS)
		} else {
			result.Reason = resulttext.LimitReason("Request failed: " + err.Error())
		}
		return result, nil // An unreachable service is health data, not an engine failure.
	}
	defer response.Body.Close()
	// Only headers are needed. Do not buffer unbounded response bodies.
	result.StatusCode = response.StatusCode
	result.Healthy = response.StatusCode == config.ExpectedStatus
	if result.Healthy {
		result.Reason = fmt.Sprintf("Received expected HTTP %d", response.StatusCode)
	} else {
		result.Reason = fmt.Sprintf("Expected HTTP %d; received HTTP %d", config.ExpectedStatus, response.StatusCode)
	}
	return result, nil
}
