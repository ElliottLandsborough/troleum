package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Handler to serve the root index page
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		serveNotFoundPage(w, r)
		return
	}
	http.ServeFile(w, r, "static/index.html")
}

func serveNotFoundPage(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile("static/404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(content)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func cacheAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Extra safety: only cache specific extensions
		ext := strings.ToLower(path.Ext(r.URL.Path))
		switch ext {
		case ".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico":
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}

		// Explicit MIME types avoid platform-dependent fallbacks like text/plain.
		switch ext {
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		}

		next.ServeHTTP(w, r)
	})
}

func serveCachedFile(filePath string) http.Handler {
	return cacheAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filePath)
	}))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(self)")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func requestScheme(r *http.Request) string {
	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		parts := strings.Split(forwardedProto, ",")
		if len(parts) > 0 {
			proto := strings.TrimSpace(parts[0])
			if proto != "" {
				return proto
			}
		}
	}

	if r.TLS != nil {
		return "https"
	}

	return "http"
}

func canonicalizeHost(host string) string {
	hostname := host
	port := ""

	parsedHost, parsedPort, err := net.SplitHostPort(host)
	if err == nil {
		hostname = parsedHost
		port = parsedPort
	}

	trimmedHostname := strings.TrimSuffix(hostname, ".")
	if trimmedHostname == hostname {
		return host
	}

	if port == "" {
		return trimmedHostname
	}

	return net.JoinHostPort(trimmedHostname, port)
}

func canonicalHostRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canonicalHost := canonicalizeHost(r.Host)
		if canonicalHost == r.Host {
			next.ServeHTTP(w, r)
			return
		}

		target := requestScheme(r) + "://" + canonicalHost + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

func setupWebServer() *http.Server {
	mux := http.NewServeMux()

	// ----------------------
	// API routes (no caching)
	// ----------------------

	// Get stations from memory.
	// Supported query params:
	// ?lat=...&lng=... sorts stations by distance to that location.
	// ?bbox=minLat,minLng,maxLat,maxLng filters stations to a bounding box.
	// ?fuel_type=E10 applies an exact fuel-type filter.
	mux.Handle("/api/stations", noStore(http.HandlerFunc(stationsAPIHandler)))

	// Get the current cached fuel type list.
	mux.Handle("/api/fuel-types", noStore(http.HandlerFunc(fuelTypesAPIHandler)))

	// Get operational stats about cache freshness, memory state, and Gov API usage.
	mux.Handle("/api/stats", noStore(http.HandlerFunc(statsAPIHandler)))

	// ----------------------
	// Root (index.html)
	// ----------------------
	mux.HandleFunc("/", rootHandler)

	// /main.css and /main.js are the new paths for the built assets, so we can serve them directly from the static directory with caching
	mux.Handle("/main.css", serveCachedFile("static/main.css"))
	mux.Handle("/main.js", serveCachedFile("static/main.js"))

	// /preview.png is the new path for the social media preview image, so we can serve it directly from the static directory with caching
	mux.Handle("/preview.png", serveCachedFile("static/preview.png"))

	// Serve favicon and touch icon assets individually.
	mux.Handle("/favicon.ico", serveCachedFile("assets/favicon.ico"))
	mux.Handle("/assets/favicon-16x16.png", serveCachedFile("assets/favicon-16x16.png"))
	mux.Handle("/assets/favicon-32x32.png", serveCachedFile("assets/favicon-32x32.png"))
	mux.Handle("/assets/favicon-48x48.png", serveCachedFile("assets/favicon-48x48.png"))
	mux.Handle("/assets/apple-touch-icon-180x180.png", serveCachedFile("assets/apple-touch-icon-180x180.png"))
	mux.Handle("/assets/apple-touch-icon.png", serveCachedFile("assets/apple-touch-icon.png"))
	mux.Handle("/assets/android-chrome-192x192.png", serveCachedFile("assets/android-chrome-192x192.png"))
	mux.Handle("/assets/android-chrome-512x512.png", serveCachedFile("assets/android-chrome-512x512.png"))

	return &http.Server{
		Addr:           "0.0.0.0:8080",
		Handler:        securityHeaders(canonicalHostRedirect(mux)),
		MaxHeaderBytes: 1 << 20,          // 1MB max for request headers (URL + all headers combined)
		ReadTimeout:    15 * time.Second, // Prevent slowloris attacks
		WriteTimeout:   15 * time.Second, // Prevent slow client writes
		IdleTimeout:    60 * time.Second, // Close idle connections
	}
}

// StartWebServer starts the web server and handles graceful shutdown
func StartWebServer(ctx context.Context) *http.Server {
	server := setupWebServer()

	// Start server in goroutine
	go func() {
		log.Println("[WEB] Starting web server on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[WEB] Web server error: %v", err)
		}
	}()

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		log.Println("[WEB] Shutting down web server gracefully...")

		// Create shutdown context with 30 second timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("[WEB] Web server shutdown error: %v", err)
		} else {
			log.Println("[WEB] Web server stopped gracefully")
		}
	}()

	return server
}
