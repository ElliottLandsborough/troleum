package main

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestHaversine(t *testing.T) {
	if got := haversine(51.5074, -0.1278, 51.5074, -0.1278); got != 0 {
		t.Fatalf("expected zero distance, got %v", got)
	}

	got := haversine(51.5074, -0.1278, 53.4808, -2.2426)
	if math.Abs(got-262.0) > 10 {
		t.Fatalf("expected rough London-Manchester distance around 262km, got %v", got)
	}
}

func TestStationsByDistance(t *testing.T) {
	stationsInput := []Station{
		{NodeID: "far", Location: Location{Latitude: 55.9533, Longitude: -3.1883}},
		{NodeID: "near", Location: Location{Latitude: 51.5090, Longitude: -0.12}},
		{NodeID: "mid", Location: Location{Latitude: 52.4862, Longitude: -1.8904}},
	}

	sorted := StationsByDistance(stationsInput, 51.5074, -0.1278)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted stations, got %d", len(sorted))
	}
	if sorted[0].NodeID != "near" || sorted[1].NodeID != "mid" || sorted[2].NodeID != "far" {
		t.Fatalf("unexpected station order: %s, %s, %s", sorted[0].NodeID, sorted[1].NodeID, sorted[2].NodeID)
	}
}

func TestContinuousFetchStationsStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	continuousFetchStations(ctx, nil, rateLimiter)
}

func TestContinuousFetchStationsSkipsWithinHourlyLimit(t *testing.T) {
	originalWait := stationsCycleWait
	originalAbortWait := stationsAbortCycleWait
	originalFetch := fetchStationsPageForCycle
	originalLast := lastStationsCycleComplete
	t.Cleanup(func() {
		stationsCycleWait = originalWait
		stationsAbortCycleWait = originalAbortWait
		fetchStationsPageForCycle = originalFetch
		lastStationsCycleComplete = originalLast
	})

	ctx, cancel := context.WithCancel(context.Background())
	calledFetch := false

	lastStationsCycleComplete = time.Now()
	stationsCycleWait = func(time.Duration) <-chan time.Time {
		cancel()
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	fetchStationsPageForCycle = func(context.Context, *OAuthClient, int, *time.Ticker) pageFetchResult {
		calledFetch = true
		return pageFetchFinalPage
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	continuousFetchStations(ctx, nil, rateLimiter)
	if calledFetch {
		t.Fatal("expected fetch not to run when cycle is skipped")
	}
}

func TestContinuousFetchStationsPageProgression(t *testing.T) {
	originalWait := stationsCycleWait
	originalAbortWait := stationsAbortCycleWait
	originalFetch := fetchStationsPageForCycle
	originalLast := lastStationsCycleComplete
	t.Cleanup(func() {
		stationsCycleWait = originalWait
		stationsAbortCycleWait = originalAbortWait
		fetchStationsPageForCycle = originalFetch
		lastStationsCycleComplete = originalLast
	})

	ctx, cancel := context.WithCancel(context.Background())
	lastStationsCycleComplete = time.Time{}

	seenPages := make([]int, 0, 2)
	fetchStationsPageForCycle = func(_ context.Context, _ *OAuthClient, page int, _ *time.Ticker) pageFetchResult {
		seenPages = append(seenPages, page)
		if len(seenPages) == 1 {
			return pageFetchContinue
		}
		cancel()
		return pageFetchFinalPage
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	continuousFetchStations(ctx, nil, rateLimiter)

	if len(seenPages) != 2 || seenPages[0] != 1 || seenPages[1] != 2 {
		t.Fatalf("expected pages [1 2], got %v", seenPages)
	}
}

func TestContinuousFetchStationsCancelDuringSkipWait(t *testing.T) {
	originalWait := stationsCycleWait
	originalAbortWait := stationsAbortCycleWait
	originalFetch := fetchStationsPageForCycle
	originalLast := lastStationsCycleComplete
	t.Cleanup(func() {
		stationsCycleWait = originalWait
		stationsAbortCycleWait = originalAbortWait
		fetchStationsPageForCycle = originalFetch
		lastStationsCycleComplete = originalLast
	})

	ctx, cancel := context.WithCancel(context.Background())
	calledFetch := false
	lastStationsCycleComplete = time.Now()

	stationsCycleWait = func(time.Duration) <-chan time.Time {
		return make(chan time.Time)
	}
	fetchStationsPageForCycle = func(context.Context, *OAuthClient, int, *time.Ticker) pageFetchResult {
		calledFetch = true
		return pageFetchFinalPage
	}

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	go cancel()
	continuousFetchStations(ctx, nil, rateLimiter)

	if calledFetch {
		t.Fatal("expected no fetch when context canceled during skip wait")
	}
}
