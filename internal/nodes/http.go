// Package nodes performs the work described by workflow steps.
package nodes

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"patchbay/internal/workflow"
)

const (
	maxHTTPHeaderBytes = 64 * 1024
	maxHTTPReasonBytes = 4 * 1024
)

type HTTP struct {
	client *http.Client
}

func NewHTTP() *HTTP {
	// Keep Go's connection, proxy, TLS, and HTTP/2 defaults without changing
	// the shared transport used by other clients.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = maxHTTPHeaderBytes
	return &HTTP{client: &http.Client{
		Transport: transport,
		// Observe the configured endpoint's response, including redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *HTTP) Execute(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
	config := step.Config
	result := workflow.HTTPResult{URL: config.URL, ExpectedStatus: config.ExpectedStatus}
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
			result.Reason = limitHTTPReason("Request failed: " + err.Error())
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

// Transport errors can include text from a malformed response. Bound the stored
// reason, including its prefix and truncation marker, before it reaches history.
func limitHTTPReason(reason string) string {
	reason = strings.ToValidUTF8(reason, "\uFFFD")
	if len(reason) <= maxHTTPReasonBytes {
		return reason
	}
	const suffix = "... [truncated]"
	end := maxHTTPReasonBytes - len(suffix)
	// Back up if the byte limit would split a multibyte character.
	for !utf8.RuneStart(reason[end]) {
		end--
	}
	return reason[:end] + suffix
}
