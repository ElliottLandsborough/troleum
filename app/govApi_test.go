package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReadCloser) Close() error             { return nil }

func TestIsRetriableStatusCode(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		if got := isRetriableStatusCode(tt.status); got != tt.want {
			t.Fatalf("isRetriableStatusCode(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestNewOAuthClientDefaults(t *testing.T) {
	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	if client == nil {
		t.Fatal("expected client instance")
	}
	if client.httpClient == nil {
		t.Fatal("expected http client to be initialized")
	}
	if client.httpClient.Timeout != 120*time.Second {
		t.Fatalf("expected timeout 120s, got %v", client.httpClient.Timeout)
	}
	if client.tokenURL != "https://example.test/token" || client.clientID != "id" || client.clientSecret != "secret" || client.scope != "scope" {
		t.Fatal("expected constructor fields to be set")
	}
}

func TestOAuthClientDoUsesCachedToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	client.httpClient = srv.Client()
	client.token = &TokenData{AccessToken: "cached-token"}
	client.expiresAt = time.Now().Add(2 * time.Minute)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("building request failed: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.StatusCode)
	}
	if gotAuth != "Bearer cached-token" {
		t.Fatalf("expected Authorization header to use cached token, got %q", gotAuth)
	}
}

func TestRequestTokenScenarios(t *testing.T) {
	t.Run("non-200 response returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("bad credentials"))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		err := client.requestToken(url.Values{"grant_type": {"client_credentials"}})
		if err == nil || !strings.Contains(err.Error(), "token request failed") {
			t.Fatalf("expected token request failed error, got %v", err)
		}
	})

	t.Run("invalid json returns decode error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json"))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		err := client.requestToken(url.Values{"grant_type": {"client_credentials"}})
		if err == nil {
			t.Fatal("expected json decode error")
		}
	})

	t.Run("success false returns token error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":false,"message":"denied"}`))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		err := client.requestToken(url.Values{"grant_type": {"client_credentials"}})
		if err == nil || !strings.Contains(err.Error(), "token error: denied") {
			t.Fatalf("expected token error, got %v", err)
		}
	})

	t.Run("success stores token and expiry", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"access_token":"new-token","token_type":"Bearer","expires_in":120,"refresh_token":"r1"}}`))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		err := client.requestToken(url.Values{"grant_type": {"client_credentials"}})
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		if client.token == nil || client.token.AccessToken != "new-token" {
			t.Fatalf("expected token to be stored, got %#v", client.token)
		}
		if time.Until(client.expiresAt) <= 0 {
			t.Fatal("expected expiresAt to be in the future")
		}
	})
}

func TestGetValidTokenRefreshAndFallback(t *testing.T) {
	t.Run("uses refresh token path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form failed: %v", err)
			}
			if r.Form.Get("grant_type") != "refresh_token" {
				t.Fatalf("expected refresh_token grant, got %q", r.Form.Get("grant_type"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"access_token":"refreshed-token","token_type":"Bearer","expires_in":120,"refresh_token":"r2"}}`))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		client.token = &TokenData{AccessToken: "old", RefreshToken: "r1"}
		client.expiresAt = time.Now().Add(-1 * time.Minute)

		tok, err := client.getValidToken()
		if err != nil {
			t.Fatalf("getValidToken returned error: %v", err)
		}
		if tok != "refreshed-token" {
			t.Fatalf("expected refreshed token, got %q", tok)
		}
	})

	t.Run("falls back to fetch token when refresh fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form failed: %v", err)
			}
			grant := r.Form.Get("grant_type")
			if grant == "refresh_token" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("refresh failed"))
				return
			}
			if grant != "client_credentials" {
				t.Fatalf("unexpected grant type %q", grant)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"access_token":"fetched-token","token_type":"Bearer","expires_in":120,"refresh_token":"r3"}}`))
		}))
		defer srv.Close()

		client := NewOAuthClient(srv.URL, "id", "secret", "scope")
		client.token = &TokenData{AccessToken: "old", RefreshToken: "r1"}
		client.expiresAt = time.Now().Add(-1 * time.Minute)

		tok, err := client.getValidToken()
		if err != nil {
			t.Fatalf("getValidToken returned error: %v", err)
		}
		if tok != "fetched-token" {
			t.Fatalf("expected fetched token fallback, got %q", tok)
		}
	})
}

func TestFetchPagesReturnFalseWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")

	if got := fetchStationsPage(ctx, client, 1, rateLimiter); got {
		t.Fatal("expected canceled stations fetch to return false")
	}
	if got := fetchPricesPage(ctx, client, 1, rateLimiter); got {
		t.Fatal("expected canceled prices fetch to return false")
	}
}

func TestFetchPagesRequestErrorQueuesRetry(t *testing.T) {
	originalQueue := globalRetryQueue
	globalRetryQueue = &RetryQueue{requests: make([]RetryRequest, 0)}
	t.Cleanup(func() { globalRetryQueue = originalQueue })

	rateLimiter := time.NewTicker(1 * time.Millisecond)
	defer rateLimiter.Stop()

	t.Run("stations request error queues retry", func(t *testing.T) {
		globalRetryQueue.requests = nil
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}))

		if got := fetchStationsPage(context.Background(), client, 9, rateLimiter); got {
			t.Fatal("expected false when stations request fails")
		}
		if len(globalRetryQueue.requests) != 1 {
			t.Fatalf("expected one queued retry, got %d", len(globalRetryQueue.requests))
		}
		if queued := globalRetryQueue.requests[0]; queued.PageNum != 9 || !queued.IsStations {
			t.Fatalf("unexpected queued request: %+v", queued)
		}
	})

	t.Run("prices request error queues retry", func(t *testing.T) {
		globalRetryQueue.requests = nil
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}))

		if got := fetchPricesPage(context.Background(), client, 10, rateLimiter); got {
			t.Fatal("expected false when prices request fails")
		}
		if len(globalRetryQueue.requests) != 1 {
			t.Fatalf("expected one queued retry, got %d", len(globalRetryQueue.requests))
		}
		if queued := globalRetryQueue.requests[0]; queued.PageNum != 10 || queued.IsStations {
			t.Fatalf("unexpected queued request: %+v", queued)
		}
	})
}

func TestFetchStationsPageStatusAndQueueBehavior(t *testing.T) {
	originalQueue := globalRetryQueue
	globalRetryQueue = &RetryQueue{requests: make([]RetryRequest, 0)}
	t.Cleanup(func() { globalRetryQueue = originalQueue })

	rateLimiter := time.NewTicker(1 * time.Millisecond)
	defer rateLimiter.Stop()

	tests := []struct {
		name       string
		statusCode int
		want       bool
		wantQueued bool
	}{
		{name: "not found is last page", statusCode: http.StatusNotFound, want: true, wantQueued: false},
		{name: "server error queued retry", statusCode: http.StatusInternalServerError, want: false, wantQueued: true},
		{name: "bad request is terminal", statusCode: http.StatusBadRequest, want: true, wantQueued: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalRetryQueue.requests = nil
			client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			}))

			got := fetchStationsPage(context.Background(), client, 3, rateLimiter)
			if got != tt.want {
				t.Fatalf("fetchStationsPage() = %v, want %v", got, tt.want)
			}
			if (len(globalRetryQueue.requests) > 0) != tt.wantQueued {
				t.Fatalf("queue state mismatch: queued=%v wantQueued=%v", len(globalRetryQueue.requests) > 0, tt.wantQueued)
			}
		})
	}
}

func TestFetchStationsPageBodyAndNodeIDBehavior(t *testing.T) {
	originalQueue := globalRetryQueue
	globalRetryQueue = &RetryQueue{requests: make([]RetryRequest, 0)}
	t.Cleanup(func() { globalRetryQueue = originalQueue })

	withTempWorkingDir(t)
	rateLimiter := time.NewTicker(1 * time.Millisecond)
	defer rateLimiter.Stop()

	t.Run("body read error queues retry", func(t *testing.T) {
		globalRetryQueue.requests = nil
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failingReadCloser{}, Header: make(http.Header)}, nil
		}))

		if got := fetchStationsPage(context.Background(), client, 1, rateLimiter); got {
			t.Fatal("expected false when body read fails")
		}
		if len(globalRetryQueue.requests) != 1 {
			t.Fatalf("expected one queued retry, got %d", len(globalRetryQueue.requests))
		}
	})

	t.Run("zero node ids treated as last page", func(t *testing.T) {
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
		}))

		if got := fetchStationsPage(context.Background(), client, 2, rateLimiter); !got {
			t.Fatal("expected true when page contains no node_id")
		}
	})

	t.Run("high node id count not last page", func(t *testing.T) {
		highCountBody := strings.Repeat("node_id,", NodeIDCountThreshold)
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(highCountBody)), Header: make(http.Header)}, nil
		}))

		if got := fetchStationsPage(context.Background(), client, 4, rateLimiter); got {
			t.Fatal("expected false when node_id count meets threshold")
		}
	})
}

func TestFetchPricesPageStatusAndBodyBehavior(t *testing.T) {
	originalQueue := globalRetryQueue
	globalRetryQueue = &RetryQueue{requests: make([]RetryRequest, 0)}
	t.Cleanup(func() { globalRetryQueue = originalQueue })

	withTempWorkingDir(t)
	rateLimiter := time.NewTicker(1 * time.Millisecond)
	defer rateLimiter.Stop()

	t.Run("status handling mirrors stations", func(t *testing.T) {
		cases := []struct {
			statusCode int
			want       bool
			wantQueued bool
		}{
			{http.StatusNotFound, true, false},
			{http.StatusTooManyRequests, false, true},
			{http.StatusBadRequest, true, false},
		}

		for _, tc := range cases {
			globalRetryQueue.requests = nil
			client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.statusCode, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}))

			got := fetchPricesPage(context.Background(), client, 6, rateLimiter)
			if got != tc.want {
				t.Fatalf("fetchPricesPage() = %v, want %v", got, tc.want)
			}
			if (len(globalRetryQueue.requests) > 0) != tc.wantQueued {
				t.Fatalf("queue state mismatch for status %d", tc.statusCode)
			}
		}
	})

	t.Run("body read error queues retry", func(t *testing.T) {
		globalRetryQueue.requests = nil
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failingReadCloser{}, Header: make(http.Header)}, nil
		}))

		if got := fetchPricesPage(context.Background(), client, 7, rateLimiter); got {
			t.Fatal("expected false when body read fails")
		}
		if len(globalRetryQueue.requests) != 1 {
			t.Fatalf("expected one queued retry, got %d", len(globalRetryQueue.requests))
		}
	})

	t.Run("high node id count not last page", func(t *testing.T) {
		highCountBody := strings.Repeat("node_id,", NodeIDCountThreshold)
		client := testOAuthClientWithRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(highCountBody)), Header: make(http.Header)}, nil
		}))

		if got := fetchPricesPage(context.Background(), client, 8, rateLimiter); got {
			t.Fatal("expected false when node_id count meets threshold")
		}
	})
}
