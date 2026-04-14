package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMainStartupAndGracefulShutdown(t *testing.T) {
	origWithCancel := mainWithCancel
	origMakeSignalChan := mainMakeSignalChan
	origSignalNotify := mainSignalNotify
	origLoadUKBoundary := mainLoadUKBoundary
	origHasUKGeofenceData := mainHasUKGeofenceData
	origInitEnrichmentTimer := mainInitEnrichmentTimer
	origLoadDataFromJSONFiles := mainLoadDataFromJSONFiles
	origLoadDotEnv := mainLoadDotEnv
	origLoadConfig := mainLoadConfig
	origStartWebServer := mainStartWebServer
	origNewOAuthClient := mainNewOAuthClient
	origStartGovAPIStatsLogger := mainStartGovAPIStatsLogger
	origNewTicker := mainNewTicker
	origContinuousFetchStations := mainContinuousFetchStations
	origContinuousFetchPrices := mainContinuousFetchPrices
	origContinuousUpdateCachedFuelTypes := mainContinuousUpdateCachedFuelTypes
	origRetryWorker := mainRetryWorker
	origSleep := mainSleep
	origLogFatal := mainLogFatal
	t.Cleanup(func() {
		mainWithCancel = origWithCancel
		mainMakeSignalChan = origMakeSignalChan
		mainSignalNotify = origSignalNotify
		mainLoadUKBoundary = origLoadUKBoundary
		mainHasUKGeofenceData = origHasUKGeofenceData
		mainInitEnrichmentTimer = origInitEnrichmentTimer
		mainLoadDataFromJSONFiles = origLoadDataFromJSONFiles
		mainLoadDotEnv = origLoadDotEnv
		mainLoadConfig = origLoadConfig
		mainStartWebServer = origStartWebServer
		mainNewOAuthClient = origNewOAuthClient
		mainStartGovAPIStatsLogger = origStartGovAPIStatsLogger
		mainNewTicker = origNewTicker
		mainContinuousFetchStations = origContinuousFetchStations
		mainContinuousFetchPrices = origContinuousFetchPrices
		mainContinuousUpdateCachedFuelTypes = origContinuousUpdateCachedFuelTypes
		mainRetryWorker = origRetryWorker
		mainSleep = origSleep
		mainLogFatal = origLogFatal
	})

	mainWithCancel = context.WithCancel
	mainMakeSignalChan = func() chan os.Signal { return make(chan os.Signal, 1) }
	mainSignalNotify = func(c chan<- os.Signal, _ ...os.Signal) {
		c <- os.Interrupt
	}
	mainLoadUKBoundary = func() {}
	mainHasUKGeofenceData = func() bool { return true }
	mainInitEnrichmentTimer = func(context.Context) {}
	mainLoadDataFromJSONFiles = func() {}
	mainLoadDotEnv = func(string) error { return errors.New("missing") }
	mainLoadConfig = func() Config {
		return Config{ClientID: "id", ClientSecret: "secret"}
	}
	mainStartWebServer = func(context.Context) *http.Server { return nil }
	mainNewOAuthClient = func(string, string, string, string) *OAuthClient { return &OAuthClient{} }
	mainStartGovAPIStatsLogger = func(context.Context, *OAuthClient, time.Duration) {}
	mainNewTicker = func(time.Duration) *time.Ticker { return time.NewTicker(1 * time.Millisecond) }
	mainContinuousFetchStations = func(context.Context, *OAuthClient, *time.Ticker) {}
	mainContinuousFetchPrices = func(context.Context, *OAuthClient, *time.Ticker) {}
	mainContinuousUpdateCachedFuelTypes = func(context.Context) {}
	mainRetryWorker = func(context.Context, *OAuthClient, *time.Ticker) {}
	mainSleep = func(time.Duration) {}
	mainLogFatal = func(...interface{}) {
		t.Fatal("mainLogFatal should not be called in success path")
	}

	main()
}

func TestMainFailsFastWhenBoundaryDataUnavailable(t *testing.T) {
	origLoadUKBoundary := mainLoadUKBoundary
	origHasUKGeofenceData := mainHasUKGeofenceData
	origLogFatal := mainLogFatal
	t.Cleanup(func() {
		mainLoadUKBoundary = origLoadUKBoundary
		mainHasUKGeofenceData = origHasUKGeofenceData
		mainLogFatal = origLogFatal
	})

	mainLoadUKBoundary = func() {}
	mainHasUKGeofenceData = func() bool { return false }
	mainLogFatal = func(...interface{}) { panic("fatal") }

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from fatal path")
		}
	}()

	main()
}
