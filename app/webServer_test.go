package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoStoreMiddleware(t *testing.T) {
	h := noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store header, got %q", w.Header().Get("Cache-Control"))
	}
}

func TestCacheAssetsMiddleware(t *testing.T) {
	tests := []struct {
		path             string
		wantCacheControl string
		wantContentType  string
	}{
		{path: "/main.css", wantCacheControl: "public, max-age=31536000, immutable", wantContentType: "text/css; charset=utf-8"},
		{path: "/main.js", wantCacheControl: "public, max-age=31536000, immutable", wantContentType: "application/javascript; charset=utf-8"},
		{path: "/preview.png", wantCacheControl: "public, max-age=31536000, immutable", wantContentType: "image/png"},
		{path: "/other.txt", wantCacheControl: "public, max-age=86400", wantContentType: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			h := cacheAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if got := w.Header().Get("Cache-Control"); got != tt.wantCacheControl {
				t.Fatalf("expected cache header %q, got %q", tt.wantCacheControl, got)
			}
			if tt.wantContentType != "" && w.Header().Get("Content-Type") != tt.wantContentType {
				t.Fatalf("expected content type %q, got %q", tt.wantContentType, w.Header().Get("Content-Type"))
			}
		})
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff, got %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("expected X-Frame-Options DENY, got %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("expected Referrer-Policy strict-origin-when-cross-origin, got %q", got)
	}
	if got := w.Header().Get("Permissions-Policy"); got != "geolocation=(self)" {
		t.Fatalf("expected Permissions-Policy geolocation=(self), got %q", got)
	}
	if got := w.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("expected Cross-Origin-Opener-Policy same-origin, got %q", got)
	}
	if got := w.Header().Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Fatalf("expected Cross-Origin-Resource-Policy same-origin, got %q", got)
	}
}

func TestCanonicalizeHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "no trailing dot", host: "troleum.org", want: "troleum.org"},
		{name: "trailing dot", host: "troleum.org.", want: "troleum.org"},
		{name: "trailing dot with port", host: "troleum.org.:8080", want: "troleum.org:8080"},
		{name: "ipv4 trailing dot", host: "127.0.0.1.", want: "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalizeHost(tt.host)
			if got != tt.want {
				t.Fatalf("canonicalizeHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestCanonicalHostRedirectMiddleware(t *testing.T) {
	h := canonicalHostRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("redirects trailing dot host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://troleum.org./path?q=1", nil)
		req.Host = "troleum.org."
		req.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()

		h.ServeHTTP(w, req)

		if w.Code != http.StatusPermanentRedirect {
			t.Fatalf("expected %d, got %d", http.StatusPermanentRedirect, w.Code)
		}
		if got := w.Header().Get("Location"); got != "https://troleum.org/path?q=1" {
			t.Fatalf("expected canonical location, got %q", got)
		}
	})

	t.Run("passes through canonical host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://troleum.org/path", nil)
		req.Host = "troleum.org"
		w := httptest.NewRecorder()

		h.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, w.Code)
		}
	})
}

func TestRootHandlerAndServeNotFoundPage(t *testing.T) {
	tempDir := withTempWorkingDir(t)
	staticDir := filepath.Join(tempDir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>index</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "404.html"), []byte("<html>missing</html>"), 0o600); err != nil {
		t.Fatalf("write 404: %v", err)
	}

	w := httptest.NewRecorder()
	rootHandler(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for root, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "index") {
		t.Fatalf("expected index body, got %q", w.Body.String())
	}

	w = httptest.NewRecorder()
	rootHandler(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing path, got %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store on 404 page, got %q", w.Header().Get("Cache-Control"))
	}
	if !strings.Contains(w.Body.String(), "missing") {
		t.Fatalf("expected 404 body, got %q", w.Body.String())
	}
}

func TestServeNotFoundPageFallsBackToHTTPNotFoundWhenFileMissing(t *testing.T) {
	withTempWorkingDir(t)

	w := httptest.NewRecorder()
	serveNotFoundPage(w, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected fallback 404, got %d", w.Code)
	}
}

func TestSetupWebServer(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	tempDir := withTempWorkingDir(t)
	staticDir := filepath.Join(tempDir, "static")
	assetsDir := filepath.Join(tempDir, "assets")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets dir: %v", err)
	}
	files := map[string]string{
		"index.html":  "<html>index</html>",
		"404.html":    "<html>missing</html>",
		"main.css":    "body{}",
		"main.js":     "console.log('ok')",
		"preview.png": "png",
		"sitemap.xml": "<?xml version=\"1.0\"?><urlset></urlset>",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(staticDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write static file %s: %v", name, err)
		}
	}
	imgFiles := map[string]string{
		"favicon.ico":                  "ico",
		"favicon-16x16.png":            "png",
		"favicon-32x32.png":            "png",
		"favicon-48x48.png":            "png",
		"apple-touch-icon-180x180.png": "png",
		"apple-touch-icon.png":         "png",
		"android-chrome-192x192.png":   "png",
		"android-chrome-512x512.png":   "png",
	}
	for name, content := range imgFiles {
		if err := os.WriteFile(filepath.Join(assetsDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write assets file %s: %v", name, err)
		}
	}

	fuelTypesCacheMutex.Lock()
	fuelTypesCache = []string{"E10"}
	fuelTypesCacheMutex.Unlock()

	srv := setupWebServer()
	if srv.Addr != "0.0.0.0:8080" {
		t.Fatalf("unexpected server addr: %s", srv.Addr)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected MaxHeaderBytes: %d", srv.MaxHeaderBytes)
	}

	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/fuel-types", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from api route, got %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store on api route, got %q", w.Header().Get("Cache-Control"))
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff on api route, got %q", got)
	}
	if got := w.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("expected Cross-Origin-Opener-Policy same-origin on api route, got %q", got)
	}

	statsClient := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	setActiveOAuthClient(statsClient)
	statsClient.statsMu.Lock()
	statsClient.statsStartedAt = time.Now().Add(-10 * time.Minute)
	statsClient.statsTotalRequests = 20
	statsClient.stats2xxCount = 12
	statsClient.stats4xxCount = 6
	statsClient.stats5xxCount = 2
	statsClient.stats401Count = 1
	statsClient.stats403Count = 3
	statsClient.statsPeakInFlight = 1
	statsClient.statsMu.Unlock()

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from stats api route, got %d", w.Code)
	}

	var statsPayload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &statsPayload); err != nil {
		t.Fatalf("stats endpoint should return valid JSON: %v", err)
	}

	dataAny, ok := statsPayload["data"]
	if !ok {
		t.Fatalf("expected data object in stats payload")
	}

	data, ok := dataAny.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be JSON object")
	}

	if _, ok := data["disk_cache"]; !ok {
		t.Fatalf("expected disk_cache section in stats payload")
	}
	if _, ok := data["memory"]; !ok {
		t.Fatalf("expected memory section in stats payload")
	}
	if _, ok := data["gov_api"]; !ok {
		t.Fatalf("expected gov_api section in stats payload")
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/main.css", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from asset route, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("expected immutable cache headers on asset route, got %q", w.Header().Get("Cache-Control"))
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("expected Referrer-Policy on asset route, got %q", got)
	}
	if got := w.Header().Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Fatalf("expected Cross-Origin-Resource-Policy on asset route, got %q", got)
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from js asset route, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/preview.png", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from png asset route, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from sitemap route, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from root favicon route, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("expected immutable cache headers on root favicon route, got %q", w.Header().Get("Cache-Control"))
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/favicon-32x32.png", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from assets asset route, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("expected immutable cache headers on assets asset route, got %q", w.Header().Get("Cache-Control"))
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/not-served.png", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from unknown assets route, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from fallback route, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/main.css?v=1", nil)
	req.Host = "troleum.org."
	req.Header.Set("X-Forwarded-Proto", "https")
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected %d for trailing-dot host redirect, got %d", http.StatusPermanentRedirect, w.Code)
	}
	if got := w.Header().Get("Location"); got != "https://troleum.org/main.css?v=1" {
		t.Fatalf("expected canonical redirect location, got %q", got)
	}
}

func TestStartWebServerReturnsServerAndHandlesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := StartWebServer(ctx)
	if srv == nil {
		t.Fatal("expected server instance")
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func TestRequestSchemeBranches(t *testing.T) {
	t.Run("uses first forwarded proto value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
		req.Header.Set("X-Forwarded-Proto", " https ,http")
		if got := requestScheme(req); got != "https" {
			t.Fatalf("expected https from forwarded proto, got %q", got)
		}
	})

	t.Run("falls back to tls when forwarded proto first value empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.test", nil)
		req.Header.Set("X-Forwarded-Proto", " ,http")
		req.TLS = &tls.ConnectionState{}
		if got := requestScheme(req); got != "https" {
			t.Fatalf("expected https from tls fallback, got %q", got)
		}
	})
}

func TestStartWebServerHandlesListenErrorPath(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		t.Skipf("could not reserve test port 8080: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := StartWebServer(ctx)
	if srv == nil {
		cancel()
		t.Fatal("expected server instance")
	}

	time.Sleep(20 * time.Millisecond)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
