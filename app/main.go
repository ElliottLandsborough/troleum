package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// RequestType represents the type of API request
type RequestType string

const (
	RequestTypeStationsPage RequestType = "stations_page"
	RequestTypePricesPage   RequestType = "prices_page"
)

// todo:
// 2. cached data from files is loaded but not saved to memory, the json files are separate from the db, and possibly useless. do we need the cached data in files at all? could we just save the raw response to the db and load from there for the enrich function? or is it useful to have the json files as a backup/cache?
// 3. work out what order the data is saved and retreived in
func main() {
	// Initialize database connection
	if err := InitDatabase(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Create context that can be cancelled for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start enriching saved pages on a 15-second timer
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		// Run enrichSavedPages immediately on startup
		enrichSavedPages()

		// Then run it every 15 seconds
		for {
			select {
			case <-ticker.C:
				enrichSavedPages()
			case <-ctx.Done():
				log.Println("Enrichment worker stopped")
				return
			}
		}
	}()

	// Start web server for saved pages
	StartWebServer(ctx)

	// load the .env file manually
	if err := loadDotEnv(".env"); err != nil {
		fmt.Println("Warning: could not load .env file:", err)
	}

	cfg := LoadConfig()

	client := NewOAuthClient(
		"https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token",
		cfg.ClientID,
		cfg.ClientSecret,
		"fuelfinder.read",
	)

	// Create rate limiter (3 requests per minute = 1 request every 20 seconds)
	rateLimiter := time.NewTicker(20 * time.Second)
	// prod allows 6 requests per minute, so use:
	// rateLimiter := time.NewTicker(10 * time.Second)
	defer rateLimiter.Stop()

	// Start continuous fetching in a goroutine
	go continuousFetchStations(client, rateLimiter)

	// Start continuous fetching of prices in a goroutine
	go continuousFetchPrices(client, rateLimiter)

	// Start retry worker in a goroutine
	go retryWorker(client, rateLimiter)

	// Keep main running and allow for other code
	log.Println("Started continuous data fetching...")
	log.Println("Press Ctrl+C or send SIGTERM to gracefully shut down")

	// Wait for shutdown signal
	<-stop
	log.Println("\nReceived shutdown signal, initiating graceful shutdown...")

	// Cancel context to signal all goroutines to stop
	cancel()

	// Give goroutines time to clean up (web server has its own 30s timeout)
	log.Println("Waiting for background workers to finish...")
	time.Sleep(2 * time.Second)

	log.Println("Shutdown complete")
}
