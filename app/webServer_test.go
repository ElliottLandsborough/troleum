package main

import (
	"context"
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
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	files := map[string]string{
		"index.html":  "<html>index</html>",
		"404.html":    "<html>missing</html>",
		"main.css":    "body{}",
		"main.js":     "console.log('ok')",
		"preview.png": "png",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(staticDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write static file %s: %v", name, err)
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
	srv.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from fallback route, got %d", w.Code)
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
