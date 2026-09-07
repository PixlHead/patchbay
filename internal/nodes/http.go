// Package nodes performs the work described by workflow steps.
package nodes

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"patchbay/internal/workflow"
)

type HTTP struct {
	client *http.Client
}

func NewHTTP() *HTTP {
	return &HTTP{client: &http.Client{
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
			result.Reason = "Request failed: " + err.Error()
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
