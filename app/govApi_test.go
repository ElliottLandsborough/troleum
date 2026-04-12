package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
