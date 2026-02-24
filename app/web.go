// a router or web handler or server that can return database data in a human readable format online
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// Handler to return latest successful stations requests from database
func savedStationsHandler(w http.ResponseWriter, r *http.Request) {
	stations, err := GetLatestSuccessfulRequests(RequestTypeStationsPage)
	if err != nil {
		http.Error(w, "Failed to get stations from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stations); err != nil {
		http.Error(w, "Failed to encode stations data", http.StatusInternalServerError)
	}
}

// Handler to return latest successful prices requests from database
func savedPricesHandler(w http.ResponseWriter, r *http.Request) {
	prices, err := GetLatestSuccessfulRequests(RequestTypePricesPage)
	if err != nil {
		http.Error(w, "Failed to get prices from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(prices); err != nil {
		http.Error(w, "Failed to encode prices data", http.StatusInternalServerError)
	}
}

// Handler to return database statistics
func dbStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := GetRequestStats()
	if err != nil {
		http.Error(w, "Failed to get database stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Failed to encode stats", http.StatusInternalServerError)
	}
}

// Handler to return most recent successful stations requests
func recentStationsHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	stations, err := GetMostRecentSuccessfulRequests(RequestTypeStationsPage, limit)
	if err != nil {
		http.Error(w, "Failed to get recent stations: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stations); err != nil {
		http.Error(w, "Failed to encode recent stations", http.StatusInternalServerError)
	}
}

// Handler to return most recent successful prices requests
func recentPricesHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	prices, err := GetMostRecentSuccessfulRequests(RequestTypePricesPage, limit)
	if err != nil {
		http.Error(w, "Failed to get recent prices: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(prices); err != nil {
		http.Error(w, "Failed to encode recent prices", http.StatusInternalServerError)
	}
}

// Handler to return the most recent successful page for stations
func latestStationPageHandler(w http.ResponseWriter, r *http.Request) {
	latestPage, err := GetMostRecentSuccessfulPage(RequestTypeStationsPage)
	if err != nil {
		http.Error(w, "Failed to get latest station page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(latestPage); err != nil {
		http.Error(w, "Failed to encode latest station page", http.StatusInternalServerError)
	}
}

// Handler to return the most recent successful page for prices
func latestPricePageHandler(w http.ResponseWriter, r *http.Request) {
	latestPage, err := GetMostRecentSuccessfulPage(RequestTypePricesPage)
	if err != nil {
		http.Error(w, "Failed to get latest price page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(latestPage); err != nil {
		http.Error(w, "Failed to encode latest price page", http.StatusInternalServerError)
	}
}

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
	mux.Handle("/saved-stations", noStore(http.HandlerFunc(savedStationsHandler)))
	mux.Handle("/saved-prices", noStore(http.HandlerFunc(savedPricesHandler)))
	mux.Handle("/db-stats", noStore(http.HandlerFunc(dbStatsHandler)))

	mux.Handle("/recent-stations", noStore(http.HandlerFunc(recentStationsHandler)))
	mux.Handle("/recent-prices", noStore(http.HandlerFunc(recentPricesHandler)))
	mux.Handle("/latest-station-page", noStore(http.HandlerFunc(latestStationPageHandler)))
	mux.Handle("/latest-price-page", noStore(http.HandlerFunc(latestPricePageHandler)))

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
