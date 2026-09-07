package nodes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"patchbay/internal/workflow"
)

func checkStep(url string) workflow.Step {
	return workflow.Step{ID: "check", Name: "Check", Type: "http.check", Config: workflow.HTTPConfig{URL: url, ExpectedStatus: 200, TimeoutMS: 100}}
}

func TestHealthResponses(t *testing.T) {
	for _, code := range []int{200, 503, 302} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(code)
			}))
			defer server.Close()
			result, err := NewHTTP().Execute(context.Background(), checkStep(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if result.StatusCode != code || result.Healthy != (code == 200) {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestUnreachableAndTimeoutAreHealthData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	result, err := NewHTTP().Execute(context.Background(), checkStep(server.URL))
	server.Close()
	if err != nil || result.Healthy || result.StatusCode != 0 || result.Reason != "No response within 100 ms" {
		t.Fatalf("timeout result: %+v, %v", result, err)
	}
	result, err = NewHTTP().Execute(context.Background(), checkStep(server.URL))
	if err != nil || result.Healthy || result.StatusCode != 0 || result.Reason == "" {
		t.Fatalf("unreachable result: %+v, %v", result, err)
	}
}

func TestParentCancellationStopsRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	_, err := NewHTTP().Execute(ctx, checkStep(server.URL))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation, got %v", err)
	}
}
