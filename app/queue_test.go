package main

import (
	"context"
	"io"
	"net/http"
	"os"
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
	client.expiresAt = time.Now().Add(2 * time.Hour)
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

func TestRetryWorkerRequeuesWhenCycleLimitApplies(t *testing.T) {
	originalQueue := globalRetryQueue
	originalTickAfter := retryWorkerTickAfter
	originalStationsProcessor := retryStationsProcessor
	originalPricesProcessor := retryPricesProcessor
	originalLastStations := lastStationsCycleComplete

	t.Cleanup(func() {
		globalRetryQueue = originalQueue
		retryWorkerTickAfter = originalTickAfter
		retryStationsProcessor = originalStationsProcessor
		retryPricesProcessor = originalPricesProcessor
		lastStationsCycleComplete = originalLastStations
	})

	globalRetryQueue = &RetryQueue{requests: []RetryRequest{{PageNum: 9, IsStations: true, AttemptCount: 1}}}
	lastStationsCycleComplete = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	retryWorkerTickAfter = func(time.Duration) <-chan time.Time {
		callCount++
		if callCount > 1 {
			cancel()
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	retryWorker(ctx, nil, rateLimiter)

	if len(globalRetryQueue.requests) != 1 {
		t.Fatalf("expected request to be re-queued, got %d", len(globalRetryQueue.requests))
	}
	if globalRetryQueue.requests[0].AttemptCount != 1 {
		t.Fatalf("expected attempt count to remain 1, got %d", globalRetryQueue.requests[0].AttemptCount)
	}
}

func TestRetryWorkerProcessOutcomeBranches(t *testing.T) {
	tests := []struct {
		name              string
		initialAttempt    int
		isStations        bool
		processorResult   bool
		wantQueueLen      int
		wantAttemptQueued int
	}{
		{name: "success drains queue", initialAttempt: 1, isStations: true, processorResult: true, wantQueueLen: 0},
		{name: "failure requeues with increment", initialAttempt: 1, isStations: true, processorResult: false, wantQueueLen: 1, wantAttemptQueued: 2},
		{name: "failure at attempt 3 gives up", initialAttempt: 3, isStations: false, processorResult: false, wantQueueLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalQueue := globalRetryQueue
			originalTickAfter := retryWorkerTickAfter
			originalStationsProcessor := retryStationsProcessor
			originalPricesProcessor := retryPricesProcessor
			originalLastStations := lastStationsCycleComplete
			originalLastPrices := lastPricesCycleComplete

			t.Cleanup(func() {
				globalRetryQueue = originalQueue
				retryWorkerTickAfter = originalTickAfter
				retryStationsProcessor = originalStationsProcessor
				retryPricesProcessor = originalPricesProcessor
				lastStationsCycleComplete = originalLastStations
				lastPricesCycleComplete = originalLastPrices
			})

			globalRetryQueue = &RetryQueue{requests: []RetryRequest{{PageNum: 11, IsStations: tt.isStations, AttemptCount: tt.initialAttempt}}}
			lastStationsCycleComplete = time.Time{}
			lastPricesCycleComplete = time.Time{}

			ctx, cancel := context.WithCancel(context.Background())
			tickFired := false
			retryWorkerTickAfter = func(time.Duration) <-chan time.Time {
				ch := make(chan time.Time, 1)
				if !tickFired {
					tickFired = true
					ch <- time.Now()
				}
				return ch
			}

			retryStationsProcessor = func(*OAuthClient, int) bool {
				cancel()
				return tt.processorResult
			}
			retryPricesProcessor = func(*OAuthClient, int) bool {
				cancel()
				return tt.processorResult
			}

			rateLimiter := time.NewTicker(1 * time.Millisecond)
			defer rateLimiter.Stop()
			retryWorker(ctx, nil, rateLimiter)

			if len(globalRetryQueue.requests) != tt.wantQueueLen {
				t.Fatalf("expected queue len %d, got %d", tt.wantQueueLen, len(globalRetryQueue.requests))
			}
			if tt.wantQueueLen == 1 && globalRetryQueue.requests[0].AttemptCount != tt.wantAttemptQueued {
				t.Fatalf("expected queued attempt count %d, got %d", tt.wantAttemptQueued, globalRetryQueue.requests[0].AttemptCount)
			}
		})
	}
}

func TestRetryWorkerNoRequestsLoopThenCancel(t *testing.T) {
	originalQueue := globalRetryQueue
	originalTickAfter := retryWorkerTickAfter
	t.Cleanup(func() {
		globalRetryQueue = originalQueue
		retryWorkerTickAfter = originalTickAfter
	})

	globalRetryQueue = &RetryQueue{requests: nil}
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	retryWorkerTickAfter = func(time.Duration) <-chan time.Time {
		callCount++
		if callCount > 1 {
			cancel()
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	retryWorker(ctx, nil, rateLimiter)
}

func TestRetryWorkerRequeuesWhenPricesLimitApplies(t *testing.T) {
	originalQueue := globalRetryQueue
	originalTickAfter := retryWorkerTickAfter
	originalLastPrices := lastPricesCycleComplete
	t.Cleanup(func() {
		globalRetryQueue = originalQueue
		retryWorkerTickAfter = originalTickAfter
		lastPricesCycleComplete = originalLastPrices
	})

	globalRetryQueue = &RetryQueue{requests: []RetryRequest{{PageNum: 5, IsStations: false, AttemptCount: 1}}}
	lastPricesCycleComplete = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	retryWorkerTickAfter = func(time.Duration) <-chan time.Time {
		callCount++
		if callCount > 1 {
			cancel()
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	retryWorker(ctx, nil, rateLimiter)

	if len(globalRetryQueue.requests) != 1 {
		t.Fatalf("expected request to be re-queued, got %d", len(globalRetryQueue.requests))
	}
}

func TestRetryWorkerContextDoneWhileWaitingRateLimiter(t *testing.T) {
	originalQueue := globalRetryQueue
	originalTickAfter := retryWorkerTickAfter
	originalStationsProcessor := retryStationsProcessor
	originalLastStations := lastStationsCycleComplete
	t.Cleanup(func() {
		globalRetryQueue = originalQueue
		retryWorkerTickAfter = originalTickAfter
		retryStationsProcessor = originalStationsProcessor
		lastStationsCycleComplete = originalLastStations
	})

	globalRetryQueue = &RetryQueue{requests: []RetryRequest{{PageNum: 12, IsStations: true, AttemptCount: 1}}}
	lastStationsCycleComplete = time.Time{}

	ctx, cancel := context.WithCancel(context.Background())
	tickFired := false
	retryWorkerTickAfter = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		if !tickFired {
			tickFired = true
			ch <- time.Now()
		}
		return ch
	}

	processorCalled := false
	retryStationsProcessor = func(*OAuthClient, int) bool {
		processorCalled = true
		return true
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	go func() {
		cancel()
	}()

	retryWorker(ctx, nil, rateLimiter)

	if processorCalled {
		t.Fatal("expected processor not to be called when context cancels before rate limiter tick")
	}
}

func TestRetryFetchStationsPageBodyReadError(t *testing.T) {
	client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingReadCloser{},
			Header:     make(http.Header),
		}, nil
	}))

	if got := retryFetchStationsPage(client, 10); got {
		t.Fatal("expected false when stations response body read fails")
	}
}

func TestRetryFetchPricesPageBodyReadError(t *testing.T) {
	client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingReadCloser{},
			Header:     make(http.Header),
		}, nil
	}))

	if got := retryFetchPricesPage(client, 10); got {
		t.Fatal("expected false when prices response body read fails")
	}
}

func TestRetryFetchPagesZeroNodeIDStillSucceed(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)

	client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
		}, nil
	}))

	if !retryFetchStationsPage(client, 20) {
		t.Fatal("expected stations retry fetch to succeed for zero-node page")
	}
	if !retryFetchPricesPage(client, 20) {
		t.Fatal("expected prices retry fetch to succeed for zero-node page")
	}

	savedStationsPagesMutex.Lock()
	_, hasStations := savedStationsPages[20]
	savedStationsPagesMutex.Unlock()
	if hasStations {
		t.Fatal("expected zero-node stations page not to be cached")
	}

	savedPricesPagesMutex.Lock()
	_, hasPrices := savedPricesPages[20]
	savedPricesPagesMutex.Unlock()
	if hasPrices {
		t.Fatal("expected zero-node prices page not to be cached")
	}
}

func TestRetryFetchPagesSaveErrorReturnsFalse(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)
	if err := os.WriteFile("json", []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write blocking json file failed: %v", err)
	}

	bodyWithNode := `[{"node_id":"station-1"}]`
	client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(bodyWithNode)),
			Header:     make(http.Header),
		}, nil
	}))

	if got := retryFetchStationsPage(client, 30); got {
		t.Fatal("expected false when stations savePageJSON fails")
	}
	if got := retryFetchPricesPage(client, 30); got {
		t.Fatal("expected false when prices savePageJSON fails")
	}
}
