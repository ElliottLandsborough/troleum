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
	JSONPreviewLength                   = 100 // Limit for previewing JSON data in logs and database
	NodeIDCountThreshold                = 500 // Threshold for considering a page to be 'full' of data
)

var (
	mainWithCancel                      = context.WithCancel
	mainMakeSignalChan                  = func() chan os.Signal { return make(chan os.Signal, 1) }
	mainSignalNotify                    = signal.Notify
	mainLoadUKBoundary                  = loadUKBoundary
	mainHasUKGeofenceData               = hasUKGeofenceData
	mainInitEnrichmentTimer             = initEnrichmentTimer
	mainLoadDataFromJSONFiles           = loadDataFromJSONFiles
	mainLoadDotEnv                      = loadDotEnv
	mainLoadConfig                      = LoadConfig
	mainStartWebServer                  = StartWebServer
	mainNewOAuthClient                  = NewOAuthClient
	mainNewTicker                       = time.NewTicker
	mainContinuousFetchStations         = continuousFetchStations
	mainContinuousFetchPrices           = continuousFetchPrices
	mainContinuousUpdateCachedFuelTypes = continuousUpdateCachedFuelTypes
	mainRetryWorker                     = retryWorker
	mainSleep                           = time.Sleep
	mainLogFatal                        = log.Fatal
)

// main is the entry point of the application
func main() {
	// Create context that can be cancelled for graceful shutdown
	ctx, cancel := mainWithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	stop := mainMakeSignalChan()
	mainSignalNotify(stop, os.Interrupt, syscall.SIGTERM)

	// Boundary data is required for coordinate normalization; fail fast if unavailable.
	mainLoadUKBoundary()
	if !mainHasUKGeofenceData() {
		mainLogFatal("UK OSM boundary data failed to load; refusing to start")
	}

	// Initialize enrichment timer BEFORE starting fetchers
	mainInitEnrichmentTimer(ctx)

	// enrich memory from json files on startup
	mainLoadDataFromJSONFiles()

	// load the .env file manually
	if err := mainLoadDotEnv(".env"); err != nil {
		fmt.Println("Warning: could not load .env file:", err)
	}

	cfg := mainLoadConfig()

	// Start web server for saved pages
	mainStartWebServer(ctx)

	// Create OAuth client
	client := mainNewOAuthClient(
		"https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token",
		cfg.ClientID,
		cfg.ClientSecret,
		"fuelfinder.read",
	)

	// Create rate limiter (3 requests per minute = 1 request every 20 seconds)
	rateLimiter := mainNewTicker(20 * time.Second)
	// prod allows 6 requests per minute, so use:
	// rateLimiter := time.NewTicker(10 * time.Second)
	defer rateLimiter.Stop()

	// Start continuous fetching in a goroutine
	go mainContinuousFetchStations(ctx, client, rateLimiter)

	// Start continuous fetching of prices in a goroutine
	go mainContinuousFetchPrices(ctx, client, rateLimiter)

	// Start continuous fuel types cache updates in a goroutine
	go mainContinuousUpdateCachedFuelTypes(ctx)

	// Start retry worker in a goroutine
	go mainRetryWorker(ctx, client, rateLimiter)

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
	mainSleep(2 * time.Second)

	log.Println("Shutdown complete")
}
