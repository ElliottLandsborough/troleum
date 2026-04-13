package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testOAuthClientWithRoundTripper(rt http.RoundTripper) *OAuthClient {
	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	client.httpClient = &http.Client{Transport: rt}
	client.token = &TokenData{AccessToken: "cached-token"}
	client.expiresAt = time.Now().Add(2 * time.Minute)
	return client
}

func TestRetryQueueAddGetHasRequests(t *testing.T) {
	rq := &RetryQueue{requests: make([]RetryRequest, 0)}
	if rq.HasRequests() {
		t.Fatal("expected empty queue")
	}

	rq.AddRequest(1, true)
	rq.AddRequest(1, true)
	rq.AddRequest(2, false)

	if len(rq.requests) != 2 {
		t.Fatalf("expected duplicate request to be ignored, got %d requests", len(rq.requests))
	}
	if !rq.HasRequests() {
		t.Fatal("expected queue to have requests")
	}

	first, ok := rq.GetNextRequest()
	if !ok || first.PageNum != 1 || !first.IsStations {
		t.Fatalf("unexpected first request: %#v, ok=%v", first, ok)
	}
	second, ok := rq.GetNextRequest()
	if !ok || second.PageNum != 2 || second.IsStations {
		t.Fatalf("unexpected second request: %#v, ok=%v", second, ok)
	}
	if _, ok := rq.GetNextRequest(); ok {
		t.Fatal("expected queue to be empty after draining")
	}
}

func TestGetRequestType(t *testing.T) {
	if got := getRequestType(true); got != "STATIONS" {
		t.Fatalf("expected STATIONS, got %q", got)
	}
	if got := getRequestType(false); got != "PRICES" {
		t.Fatalf("expected PRICES, got %q", got)
	}
}

func TestRetryWorkerReturnsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	retryWorker(ctx, nil, rateLimiter)
}

func TestRetryFetchStationsPageStatusHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{name: "not found treated terminal", statusCode: http.StatusNotFound, want: true},
		{name: "server error retriable", statusCode: http.StatusInternalServerError, want: false},
		{name: "bad request non-retriable", statusCode: http.StatusBadRequest, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			}))

			if got := retryFetchStationsPage(client, 1); got != tt.want {
				t.Fatalf("retryFetchStationsPage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryFetchPricesPageStatusHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{name: "not found treated terminal", statusCode: http.StatusNotFound, want: true},
		{name: "server error retriable", statusCode: http.StatusInternalServerError, want: false},
		{name: "bad request non-retriable", statusCode: http.StatusBadRequest, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			}))

			if got := retryFetchPricesPage(client, 1); got != tt.want {
				t.Fatalf("retryFetchPricesPage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryFetchPagesSuccessStoresCachedPages(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)

	bodyByPath := map[string]string{
		"/api/v1/pfs":             testStationPageJSON,
		"/api/v1/pfs/fuel-prices": testPricePageJSON,
	}

	client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := bodyByPath[req.URL.Path]
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}))

	if !retryFetchStationsPage(client, 1) {
		t.Fatal("expected retryFetchStationsPage success")
	}
	if !retryFetchPricesPage(client, 1) {
		t.Fatal("expected retryFetchPricesPage success")
	}

	savedStationsPagesMutex.Lock()
	_, hasStations := savedStationsPages[1]
	savedStationsPagesMutex.Unlock()
	if !hasStations {
		t.Fatal("expected stations page to be cached")
	}

	savedPricesPagesMutex.Lock()
	_, hasPrices := savedPricesPages[1]
	savedPricesPagesMutex.Unlock()
	if !hasPrices {
		t.Fatal("expected prices page to be cached")
	}
}
