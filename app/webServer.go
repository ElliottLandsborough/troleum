package main

import (
	"context"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

// Handler to serve the root index page
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "static/index.html")
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
		case ".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}

		next.ServeHTTP(w, r)
	})
}

func setupWebServer() *http.Server {
	mux := http.NewServeMux()

	// ----------------------
	// API routes (no caching)
	// ----------------------

	// Get saved stations/prices PAGES from database. All pages returned
	mux.Handle("/admin/saved-stations-pages", noStore(http.HandlerFunc(savedStationsHandler)))
	mux.Handle("/admin/saved-prices-pages", noStore(http.HandlerFunc(savedPricesHandler)))

	// Get database stats - todo: more stats like total requests, total successful, success rate, etc
	// todo: estimated total stations/prices based on: db, memory
	// todo: response:200 count per page, non:200 count per page
	mux.Handle("/admin/db-stats", noStore(http.HandlerFunc(dbStatsHandler)))

	// Get the 10 most recent successful stations/prices PAGES requests (with optional ?limit= query param)
	mux.Handle("/admin/recent-stations", noStore(http.HandlerFunc(recentStationsHandler)))
	mux.Handle("/admin/recent-prices", noStore(http.HandlerFunc(recentPricesHandler)))

	// Get the most recent successful stations/prices PAGE request
	// (just the latest page fetched successfully, but the data field might be 500 stations or prices)
	mux.Handle("/admin/latest-station-page", noStore(http.HandlerFunc(latestStationPageHandler)))
	mux.Handle("/admin/latest-price-page", noStore(http.HandlerFunc(latestPricePageHandler)))

	// get all stations, with pagination, from memory
	// ?page = 1,2,3... (default 1)
	// ?per_page = 1,2,3... (default 20)
	// ?location = lat,lng (optional, if provided, will return stations sorted by distance to this location)
	// ?fuel_type = e5,e10,diesel (optional, if provided, will filter stations by fuel type)
	mux.Handle("/stations", noStore(http.HandlerFunc(stationsAPIHandler)))

	// ----------------------
	// Static asset routes
	// ----------------------
	jsFS := http.FileServer(http.Dir("static/js"))
	cssFS := http.FileServer(http.Dir("static/css"))
	imgFS := http.FileServer(http.Dir("static/img"))

	mux.Handle("/js/",
		cacheAssets(
			http.StripPrefix("/js/", jsFS)))

	mux.Handle("/css/",
		cacheAssets(
			http.StripPrefix("/css/", cssFS)))

	mux.Handle("/img/",
		cacheAssets(
			http.StripPrefix("/img/", imgFS)))

	// ----------------------
	// Root (index.html)
	// ----------------------
	mux.HandleFunc("/", rootHandler)

	return &http.Server{
		Addr:    "0.0.0.0:8080",
		Handler: mux,
	}
}

// StartWebServer starts the web server and handles graceful shutdown
func StartWebServer(ctx context.Context) *http.Server {
	server := setupWebServer()

	// Start server in goroutine
	go func() {
		log.Println("Starting web server on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		log.Println("Shutting down web server gracefully...")

		// Create shutdown context with 30 second timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Web server shutdown error: %v", err)
		} else {
			log.Println("Web server stopped gracefully")
		}
	}()

	return server
}
