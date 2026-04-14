package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsAPIHandlerSuccess(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)

	if err := os.MkdirAll("json", 0o755); err != nil {
		t.Fatalf("mkdir json: %v", err)
	}
	if err := os.WriteFile(filepath.Join("json", "stations_page_1.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write json file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	statsAPIHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp statsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected code %d in payload, got %d", http.StatusOK, resp.Code)
	}
	if resp.Data.GeneratedAt == "" {
		t.Fatal("expected generated_at to be populated")
	}
}

func TestStatsAPIHandlerWriteFailure(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	fw := &failingResponseWriter{}

	statsAPIHandler(fw, req)

	if fw.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("expected error response content type, got %q", fw.Header().Get("Content-Type"))
	}
}

func TestCollectDiskCacheStatsMissingDirectory(t *testing.T) {
	withTempWorkingDir(t)

	now := time.Now()
	stats := collectDiskCacheStats(now)

	if stats.JSONFileCount != 0 {
		t.Fatalf("expected 0 json files when directory is missing, got %d", stats.JSONFileCount)
	}
	if stats.OldestFileName != "" {
		t.Fatalf("expected no oldest file name, got %q", stats.OldestFileName)
	}
}

func TestCollectDiskCacheStatsWithFiles(t *testing.T) {
	withTempWorkingDir(t)

	if err := os.MkdirAll("json", 0o755); err != nil {
		t.Fatalf("mkdir json: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	oldestPath := filepath.Join("json", "stations_page_1.json")
	newestPath := filepath.Join("json", "prices_page_2.json")
	if err := os.WriteFile(oldestPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write oldest file: %v", err)
	}
	if err := os.WriteFile(newestPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write newest file: %v", err)
	}
	if err := os.WriteFile(filepath.Join("json", "README.txt"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write non-json file: %v", err)
	}

	oldestMod := now.Add(-20 * time.Minute)
	newestMod := now.Add(-2 * time.Minute)
	if err := os.Chtimes(oldestPath, oldestMod, oldestMod); err != nil {
		t.Fatalf("chtimes oldest file: %v", err)
	}
	if err := os.Chtimes(newestPath, newestMod, newestMod); err != nil {
		t.Fatalf("chtimes newest file: %v", err)
	}

	stats := collectDiskCacheStats(now)
	if stats.JSONFileCount != 2 {
		t.Fatalf("expected 2 json files, got %d", stats.JSONFileCount)
	}
	if stats.OldestFileName != "stations_page_1.json" {
		t.Fatalf("expected oldest file stations_page_1.json, got %q", stats.OldestFileName)
	}
	if stats.NewestFileName != "prices_page_2.json" {
		t.Fatalf("expected newest file prices_page_2.json, got %q", stats.NewestFileName)
	}
	if stats.OldestFileAgeSeconds < 1199 || stats.OldestFileAgeSeconds > 1201 {
		t.Fatalf("expected oldest age around 1200s, got %d", stats.OldestFileAgeSeconds)
	}
	if stats.NewestFileAgeSeconds < 119 || stats.NewestFileAgeSeconds > 121 {
		t.Fatalf("expected newest age around 120s, got %d", stats.NewestFileAgeSeconds)
	}
}

func TestCollectDiskCacheStatsNoJSONFiles(t *testing.T) {
	withTempWorkingDir(t)

	if err := os.MkdirAll("json", 0o755); err != nil {
		t.Fatalf("mkdir json: %v", err)
	}
	if err := os.WriteFile(filepath.Join("json", "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write non-json file: %v", err)
	}

	stats := collectDiskCacheStats(time.Now())
	if stats.JSONFileCount != 0 {
		t.Fatalf("expected 0 json files, got %d", stats.JSONFileCount)
	}
}

func TestCollectMemoryStats(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	now := time.Now().UTC().Truncate(time.Second)

	stationsMutex.Lock()
	stations = []Station{{NodeID: "s1"}, {NodeID: "s2"}, {NodeID: "s3"}}
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStations = []PriceStation{
		{NodeID: "s1", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 145.9}, {FuelType: "B7", Price: 154.9}}},
		{NodeID: "s2", FuelPrices: []FuelPrice{{FuelType: "E5", Price: 159.9}}},
	}
	priceStationsMutex.Unlock()

	stationLocationsMutex.Lock()
	stationLocations = map[string]LatLon{"s1": {Lat: 1, Lon: 1}, "s2": {Lat: 2, Lon: 2}}
	stationLocationsMutex.Unlock()

	fuelTypesCacheMutex.Lock()
	fuelTypesCache = []string{"E10", "E5", "B7"}
	fuelTypesCacheMutex.Unlock()

	savedStationsPagesMutex.Lock()
	savedStationsPages[1] = ResponseCache{CreatedAt: now.Add(-30 * time.Minute)}
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	savedPricesPages[1] = ResponseCache{CreatedAt: now.Add(-10 * time.Minute)}
	savedPricesPagesMutex.Unlock()

	stats := collectMemoryStats(now)
	if stats.StationsCount != 3 {
		t.Fatalf("expected 3 stations, got %d", stats.StationsCount)
	}
	if stats.PriceStationsCount != 2 {
		t.Fatalf("expected 2 price stations, got %d", stats.PriceStationsCount)
	}
	if stats.StationPriceEntriesCount != 3 {
		t.Fatalf("expected 3 station price entries, got %d", stats.StationPriceEntriesCount)
	}
	if stats.StationLocationsCount != 2 {
		t.Fatalf("expected 2 station locations, got %d", stats.StationLocationsCount)
	}
	if stats.FuelTypesCachedCount != 3 {
		t.Fatalf("expected 3 cached fuel types, got %d", stats.FuelTypesCachedCount)
	}
	if stats.CachedStationPagesCount != 1 {
		t.Fatalf("expected 1 cached station page, got %d", stats.CachedStationPagesCount)
	}
	if stats.CachedPricePagesCount != 1 {
		t.Fatalf("expected 1 cached price page, got %d", stats.CachedPricePagesCount)
	}
	if stats.OldestCachedPageAgeSeconds < 1799 || stats.OldestCachedPageAgeSeconds > 1801 {
		t.Fatalf("expected oldest cached page age around 1800s, got %d", stats.OldestCachedPageAgeSeconds)
	}
}

func TestCollectGovAPIStatsUnavailable(t *testing.T) {
	setActiveOAuthClient(nil)

	stats := collectGovAPIStats(time.Now())
	if stats.StatsAvailable {
		t.Fatal("expected stats to be unavailable without active OAuth client")
	}
}

func TestCollectGovAPIStatsAvailable(t *testing.T) {
	setActiveOAuthClient(nil)
	t.Cleanup(func() { setActiveOAuthClient(nil) })

	now := time.Now().UTC().Truncate(time.Second)
	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	client.statsMu.Lock()
	client.statsStartedAt = now.Add(-10 * time.Minute)
	client.statsTotalRequests = 30
	client.stats2xxCount = 20
	client.stats4xxCount = 8
	client.stats5xxCount = 2
	client.stats401Count = 1
	client.stats403Count = 3
	client.statsNetworkErrors = 4
	client.statsInFlight = 1
	client.statsPeakInFlight = 1
	client.statsMu.Unlock()
	setActiveOAuthClient(client)

	stats := collectGovAPIStats(now)
	if !stats.StatsAvailable {
		t.Fatal("expected stats to be available")
	}
	if stats.TotalRequests != 30 {
		t.Fatalf("expected total requests 30, got %d", stats.TotalRequests)
	}
	if stats.Requests403 != 3 {
		t.Fatalf("expected 3 forbidden responses, got %d", stats.Requests403)
	}
	if stats.AvgRequestsPerMinute < 2.99 || stats.AvgRequestsPerMinute > 3.01 {
		t.Fatalf("expected avg requests/min around 3.0, got %.4f", stats.AvgRequestsPerMinute)
	}
	if stats.PercentOf30RPMLimit < 9.9 || stats.PercentOf30RPMLimit > 10.1 {
		t.Fatalf("expected ~10%% of 30 rpm limit, got %.4f", stats.PercentOf30RPMLimit)
	}
}

func TestCollectTimerStatsAndRuntimeStats(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	now := time.Now().UTC().Truncate(time.Second)

	enrichmentTimerMutex.Lock()
	enrichmentTimer = time.NewTimer(time.Hour)
	enrichmentNextRunAt = now.Add(30 * time.Minute)
	enrichmentTimerMutex.Unlock()

	cycleTimeMutex.Lock()
	lastPricesCycleComplete = now.Add(-5 * time.Minute)
	lastStationsCycleComplete = now.Add(-70 * time.Minute)
	cycleTimeMutex.Unlock()

	dynamicMaxPagesMutex.Lock()
	pricesMaxPagesPerCycleCap = 17
	stationsMaxPagesPerCycleCap = 22
	dynamicMaxPagesMutex.Unlock()

	globalRetryQueue.mu.Lock()
	globalRetryQueue.requests = []RetryRequest{{PageNum: 9, IsStations: true}}
	globalRetryQueue.mu.Unlock()

	timerStats := collectTimerStats(now)
	if !timerStats.Enrichment.IsScheduled {
		t.Fatal("expected enrichment timer to be scheduled")
	}
	if timerStats.Enrichment.SecondsUntilNextRun < 1799 || timerStats.Enrichment.SecondsUntilNextRun > 1801 {
		t.Fatalf("expected enrichment run in ~1800s, got %d", timerStats.Enrichment.SecondsUntilNextRun)
	}
	if !timerStats.PricesCycleCooldown.InCooldown {
		t.Fatal("expected prices cycle to still be in cooldown")
	}
	if timerStats.StationsCycleCooldown.InCooldown {
		t.Fatal("expected stations cycle cooldown to have elapsed")
	}

	runtimeStats := collectRuntimeStats()
	if runtimeStats.RetryQueueLength != 1 {
		t.Fatalf("expected retry queue length 1, got %d", runtimeStats.RetryQueueLength)
	}
	if runtimeStats.PricesMaxPagesPerCycleCap != 17 {
		t.Fatalf("expected prices cap 17, got %d", runtimeStats.PricesMaxPagesPerCycleCap)
	}
	if runtimeStats.StationsMaxPagesPerCycleCap != 22 {
		t.Fatalf("expected stations cap 22, got %d", runtimeStats.StationsMaxPagesPerCycleCap)
	}
}

func TestEvaluateStatsHealthOK(t *testing.T) {
	disk := diskCacheInfo{
		JSONFileCount:        10,
		OldestFileAgeSeconds: int64((30 * time.Minute).Seconds()),
	}
	memory := memoryInfo{
		CachedStationPagesCount:    5,
		CachedPricePagesCount:      5,
		OldestCachedPageAgeSeconds: int64((45 * time.Minute).Seconds()),
	}
	gov := govAPIInfo{
		StatsAvailable: true,
		TotalRequests:  100,
		Requests4xx:    5,
		Requests403:    1,
		NetworkErrors:  2,
	}

	health := evaluateStatsHealth(disk, memory, gov)
	if health.Status != "ok" {
		t.Fatalf("expected health status ok, got %q", health.Status)
	}
	if len(health.Reasons) != 0 {
		t.Fatalf("expected no health reasons, got %v", health.Reasons)
	}
}

func TestEvaluateStatsHealthWarns(t *testing.T) {
	disk := diskCacheInfo{
		JSONFileCount:        0,
		OldestFileAgeSeconds: int64((3 * time.Hour).Seconds()),
	}
	memory := memoryInfo{
		CachedStationPagesCount:    0,
		CachedPricePagesCount:      0,
		OldestCachedPageAgeSeconds: int64((3 * time.Hour).Seconds()),
	}
	gov := govAPIInfo{
		StatsAvailable: true,
		TotalRequests:  10,
		Requests4xx:    6,
		Requests403:    3,
		NetworkErrors:  2,
	}

	health := evaluateStatsHealth(disk, memory, gov)
	if health.Status != "warn" {
		t.Fatalf("expected health status warn, got %q", health.Status)
	}

	reasons := map[string]bool{}
	for _, reason := range health.Reasons {
		reasons[reason] = true
	}

	wantReasons := []string{
		"no_json_cache_files_found",
		"no_cached_pages_in_memory",
		"high_403_ratio",
		"high_4xx_ratio",
		"high_network_error_ratio",
	}

	for _, want := range wantReasons {
		if !reasons[want] {
			t.Fatalf("expected health reason %q in %v", want, health.Reasons)
		}
	}
}

func TestBuildScheduledTimerInfoAndCooldownInfoEdgeCases(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	notScheduled := buildScheduledTimerInfo(false, now.Add(time.Minute), now)
	if notScheduled.IsScheduled {
		t.Fatal("expected unscheduled timer info")
	}

	pastDue := buildScheduledTimerInfo(true, now.Add(-time.Minute), now)
	if pastDue.SecondsUntilNextRun != 0 {
		t.Fatalf("expected 0 seconds for past-due timer, got %d", pastDue.SecondsUntilNextRun)
	}

	zeroCooldown := buildCooldownInfo(time.Time{}, 15*time.Minute, now)
	if zeroCooldown.CooldownDurationSeconds != int64((15 * time.Minute).Seconds()) {
		t.Fatalf("unexpected cooldown duration seconds: %d", zeroCooldown.CooldownDurationSeconds)
	}
	if zeroCooldown.InCooldown {
		t.Fatal("expected zero-time cooldown info to not be in cooldown")
	}

	pastCompleted := buildCooldownInfo(now.Add(-2*time.Hour), 15*time.Minute, now)
	if pastCompleted.InCooldown {
		t.Fatal("expected cooldown to be elapsed")
	}
	if pastCompleted.SecondsUntilNextEligibleRun != 0 {
		t.Fatalf("expected 0 seconds remaining after elapsed cooldown, got %d", pastCompleted.SecondsUntilNextEligibleRun)
	}
}

func TestEvaluateStatsHealthWarnsOnStaleDataAndUnavailableGovStats(t *testing.T) {
	disk := diskCacheInfo{
		JSONFileCount:        1,
		OldestFileAgeSeconds: int64((3 * time.Hour).Seconds()),
	}
	memory := memoryInfo{
		CachedStationPagesCount:    1,
		CachedPricePagesCount:      0,
		OldestCachedPageAgeSeconds: int64((3 * time.Hour).Seconds()),
	}
	gov := govAPIInfo{StatsAvailable: false}

	health := evaluateStatsHealth(disk, memory, gov)
	if health.Status != "warn" {
		t.Fatalf("expected health status warn, got %q", health.Status)
	}

	reasons := map[string]bool{}
	for _, reason := range health.Reasons {
		reasons[reason] = true
	}

	wantReasons := []string{
		"oldest_json_file_is_stale",
		"oldest_cached_page_in_memory_is_stale",
		"gov_api_stats_unavailable",
	}

	for _, want := range wantReasons {
		if !reasons[want] {
			t.Fatalf("expected health reason %q in %v", want, health.Reasons)
		}
	}
}
