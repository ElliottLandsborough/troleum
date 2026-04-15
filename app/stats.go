package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"time"
)

const (
	statsWarnOldestJSONFileAge        = 2 * time.Hour
	statsWarnOldestMemoryCacheAge     = 2 * time.Hour
	statsWarn403RatioPercent          = 20.0
	statsWarn4xxRatioPercent          = 40.0
	statsWarnNetworkErrorRatioPercent = 10.0
)

type statsResponse struct {
	Code int       `json:"code"`
	Data statsData `json:"data"`
}

type statsData struct {
	GeneratedAt string        `json:"generated_at"`
	Health      healthInfo    `json:"health"`
	DiskCache   diskCacheInfo `json:"disk_cache"`
	Memory      memoryInfo    `json:"memory"`
	GovAPI      govAPIInfo    `json:"gov_api"`
	Timers      timersInfo    `json:"timers"`
	Runtime     runtimeInfo   `json:"runtime"`
}

type healthInfo struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

type diskCacheInfo struct {
	JSONFileCount                int    `json:"json_file_count"`
	StationsJSONFileCount        int    `json:"stations_json_file_count"`
	PricesJSONFileCount          int    `json:"prices_json_file_count"`
	OldestStationsFileName       string `json:"oldest_stations_file_name,omitempty"`
	OldestStationsFileModifiedAt string `json:"oldest_stations_file_modified_at,omitempty"`
	OldestStationsFileAgeSeconds int64  `json:"oldest_stations_file_age_seconds"`
	OldestStationsFileAgeHuman   string `json:"oldest_stations_file_age_human"`
	NewestStationsFileName       string `json:"newest_stations_file_name,omitempty"`
	NewestStationsFileModifiedAt string `json:"newest_stations_file_modified_at,omitempty"`
	NewestStationsFileAgeSeconds int64  `json:"newest_stations_file_age_seconds"`
	NewestStationsFileAgeHuman   string `json:"newest_stations_file_age_human"`
	OldestPricesFileName         string `json:"oldest_prices_file_name,omitempty"`
	OldestPricesFileModifiedAt   string `json:"oldest_prices_file_modified_at,omitempty"`
	OldestPricesFileAgeSeconds   int64  `json:"oldest_prices_file_age_seconds"`
	OldestPricesFileAgeHuman     string `json:"oldest_prices_file_age_human"`
	NewestPricesFileName         string `json:"newest_prices_file_name,omitempty"`
	NewestPricesFileModifiedAt   string `json:"newest_prices_file_modified_at,omitempty"`
	NewestPricesFileAgeSeconds   int64  `json:"newest_prices_file_age_seconds"`
	NewestPricesFileAgeHuman     string `json:"newest_prices_file_age_human"`
	OldestFileName               string `json:"oldest_file_name,omitempty"`
	OldestFileModifiedAt         string `json:"oldest_file_modified_at,omitempty"`
	OldestFileAgeSeconds         int64  `json:"oldest_file_age_seconds"`
	OldestFileAgeHuman           string `json:"oldest_file_age_human"`
	NewestFileName               string `json:"newest_file_name,omitempty"`
	NewestFileModifiedAt         string `json:"newest_file_modified_at,omitempty"`
	NewestFileAgeSeconds         int64  `json:"newest_file_age_seconds"`
	NewestFileAgeHuman           string `json:"newest_file_age_human"`
}

type memoryInfo struct {
	StationsCount              int    `json:"stations_count"`
	PriceStationsCount         int    `json:"price_stations_count"`
	StationPriceEntriesCount   int    `json:"station_price_entries_count"`
	StationLocationsCount      int    `json:"station_locations_count"`
	FuelTypesCachedCount       int    `json:"fuel_types_cached_count"`
	CachedStationPagesCount    int    `json:"cached_station_pages_count"`
	CachedPricePagesCount      int    `json:"cached_price_pages_count"`
	OldestCachedPageAgeSeconds int64  `json:"oldest_cached_page_age_seconds"`
	OldestCachedPageAgeHuman   string `json:"oldest_cached_page_age_human"`
}

type govAPIInfo struct {
	StatsAvailable       bool    `json:"stats_available"`
	StatsSince           string  `json:"stats_since,omitempty"`
	TotalRequests        int     `json:"total_requests"`
	Requests2xx          int     `json:"requests_2xx"`
	Requests4xx          int     `json:"requests_4xx"`
	Requests5xx          int     `json:"requests_5xx"`
	Requests401          int     `json:"requests_401"`
	Requests403          int     `json:"requests_403"`
	NetworkErrors        int     `json:"network_errors"`
	InFlightRequests     int     `json:"in_flight_requests"`
	PeakInFlightRequests int     `json:"peak_in_flight_requests"`
	AvgRequestsPerMinute float64 `json:"avg_requests_per_minute"`
	PercentOf30RPMLimit  float64 `json:"percent_of_30_rpm_limit"`
}

type timersInfo struct {
	Enrichment            scheduledTimerInfo `json:"enrichment"`
	PricesCycleCooldown   cooldownInfo       `json:"prices_cycle_cooldown"`
	StationsCycleCooldown cooldownInfo       `json:"stations_cycle_cooldown"`
}

type scheduledTimerInfo struct {
	IsScheduled         bool   `json:"is_scheduled"`
	NextRunAt           string `json:"next_run_at,omitempty"`
	SecondsUntilNextRun int64  `json:"seconds_until_next_run"`
	HumanUntilNextRun   string `json:"human_until_next_run"`
}

type cooldownInfo struct {
	LastCompletedAt             string `json:"last_completed_at,omitempty"`
	CooldownDurationSeconds     int64  `json:"cooldown_duration_seconds"`
	InCooldown                  bool   `json:"in_cooldown"`
	NextEligibleRunAt           string `json:"next_eligible_run_at,omitempty"`
	SecondsUntilNextEligibleRun int64  `json:"seconds_until_next_eligible_run"`
	HumanUntilNextEligibleRun   string `json:"human_until_next_eligible_run"`
}

type runtimeInfo struct {
	RetryQueueLength            int    `json:"retry_queue_length"`
	PricesMaxPagesPerCycleCap   int    `json:"prices_max_pages_per_cycle_cap"`
	StationsMaxPagesPerCycleCap int    `json:"stations_max_pages_per_cycle_cap"`
	ProcessStartedAt            string `json:"process_started_at"`
	ProcessUptimeSeconds        int64  `json:"process_uptime_seconds"`
	ProcessUptimeHuman          string `json:"process_uptime_human"`
	RAMSysBytes                 uint64 `json:"ram_sys_bytes"`
	RAMSysHuman                 string `json:"ram_sys_human"`
	RAMHeapAllocBytes           uint64 `json:"ram_heap_alloc_bytes"`
	RAMHeapAllocHuman           string `json:"ram_heap_alloc_human"`
	RAMNextGCBytes              uint64 `json:"ram_next_gc_bytes"`
	RAMNextGCHuman              string `json:"ram_next_gc_human"`
	RAMGCCycles                 uint32 `json:"ram_gc_cycles"`
	RAMGCCyclesHuman            string `json:"ram_gc_cycles_human"`
}

var runtimeStatsProcessStartedAt = time.Now()

func statsAPIHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	disk := collectDiskCacheStats(now)
	memory := collectMemoryStats(now)
	gov := collectGovAPIStats(now)
	timers := collectTimerStats(now)
	runtime := collectRuntimeStats(now)
	health := evaluateStatsHealth(disk, memory, gov)

	response := statsResponse{
		Code: http.StatusOK,
		Data: statsData{
			GeneratedAt: now.UTC().Format(time.RFC3339),
			Health:      health,
			DiskCache:   disk,
			Memory:      memory,
			GovAPI:      gov,
			Timers:      timers,
			Runtime:     runtime,
		},
	}

	if err := writeJSONPretty(w, response); err != nil {
		http.Error(w, "Failed to encode stats response", http.StatusInternalServerError)
		return
	}
}

func collectDiskCacheStats(now time.Time) diskCacheInfo {
	entries, err := os.ReadDir("json")
	if err != nil {
		return diskCacheInfo{}
	}

	oldestTime := now
	newestTime := time.Time{}
	oldestName := ""
	newestName := ""
	count := 0
	stationsCount := 0
	pricesCount := 0
	oldestStationsTime := now
	newestStationsTime := time.Time{}
	oldestStationsName := ""
	newestStationsName := ""
	oldestPricesTime := now
	newestPricesTime := time.Time{}
	oldestPricesName := ""
	newestPricesName := ""

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		mod := info.ModTime()
		if count == 0 || mod.Before(oldestTime) {
			oldestTime = mod
			oldestName = entry.Name()
		}
		if count == 0 || mod.After(newestTime) {
			newestTime = mod
			newestName = entry.Name()
		}

		switch {
		case strings.HasPrefix(entry.Name(), "stations_"):
			stationsCount++
			if stationsCount == 1 || mod.Before(oldestStationsTime) {
				oldestStationsTime = mod
				oldestStationsName = entry.Name()
			}
			if stationsCount == 1 || mod.After(newestStationsTime) {
				newestStationsTime = mod
				newestStationsName = entry.Name()
			}
		case strings.HasPrefix(entry.Name(), "prices_"):
			pricesCount++
			if pricesCount == 1 || mod.Before(oldestPricesTime) {
				oldestPricesTime = mod
				oldestPricesName = entry.Name()
			}
			if pricesCount == 1 || mod.After(newestPricesTime) {
				newestPricesTime = mod
				newestPricesName = entry.Name()
			}
		}

		count++
	}

	if count == 0 {
		return diskCacheInfo{}
	}

	oldestAge := now.Sub(oldestTime)
	if oldestAge < 0 {
		oldestAge = 0
	}
	newestAge := now.Sub(newestTime)
	if newestAge < 0 {
		newestAge = 0
	}

	oldestStationsAge, newestStationsAge := categorizedFileAges(now, oldestStationsTime, newestStationsTime, stationsCount)
	oldestPricesAge, newestPricesAge := categorizedFileAges(now, oldestPricesTime, newestPricesTime, pricesCount)

	return diskCacheInfo{
		JSONFileCount:                count,
		StationsJSONFileCount:        stationsCount,
		PricesJSONFileCount:          pricesCount,
		OldestStationsFileName:       filepath.Base(oldestStationsName),
		OldestStationsFileModifiedAt: formatStatsTime(oldestStationsTime, stationsCount),
		OldestStationsFileAgeSeconds: int64(oldestStationsAge.Seconds()),
		OldestStationsFileAgeHuman:   oldestStationsAge.Round(time.Second).String(),
		NewestStationsFileName:       filepath.Base(newestStationsName),
		NewestStationsFileModifiedAt: formatStatsTime(newestStationsTime, stationsCount),
		NewestStationsFileAgeSeconds: int64(newestStationsAge.Seconds()),
		NewestStationsFileAgeHuman:   newestStationsAge.Round(time.Second).String(),
		OldestPricesFileName:         filepath.Base(oldestPricesName),
		OldestPricesFileModifiedAt:   formatStatsTime(oldestPricesTime, pricesCount),
		OldestPricesFileAgeSeconds:   int64(oldestPricesAge.Seconds()),
		OldestPricesFileAgeHuman:     oldestPricesAge.Round(time.Second).String(),
		NewestPricesFileName:         filepath.Base(newestPricesName),
		NewestPricesFileModifiedAt:   formatStatsTime(newestPricesTime, pricesCount),
		NewestPricesFileAgeSeconds:   int64(newestPricesAge.Seconds()),
		NewestPricesFileAgeHuman:     newestPricesAge.Round(time.Second).String(),
		OldestFileName:               filepath.Base(oldestName),
		OldestFileModifiedAt:         oldestTime.UTC().Format(time.RFC3339),
		OldestFileAgeSeconds:         int64(oldestAge.Seconds()),
		OldestFileAgeHuman:           oldestAge.Round(time.Second).String(),
		NewestFileName:               filepath.Base(newestName),
		NewestFileModifiedAt:         newestTime.UTC().Format(time.RFC3339),
		NewestFileAgeSeconds:         int64(newestAge.Seconds()),
		NewestFileAgeHuman:           newestAge.Round(time.Second).String(),
	}
}

func categorizedFileAges(now, oldest, newest time.Time, count int) (time.Duration, time.Duration) {
	if count == 0 {
		return 0, 0
	}

	oldestAge := now.Sub(oldest)
	if oldestAge < 0 {
		oldestAge = 0
	}

	newestAge := now.Sub(newest)
	if newestAge < 0 {
		newestAge = 0
	}

	return oldestAge, newestAge
}

func formatStatsTime(modifiedAt time.Time, count int) string {
	if count == 0 || modifiedAt.IsZero() {
		return ""
	}

	return modifiedAt.UTC().Format(time.RFC3339)
}

func collectMemoryStats(now time.Time) memoryInfo {
	stationsMutex.Lock()
	stationsCount := len(stations)
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStationsCount := len(priceStations)
	priceEntryCount := 0
	for _, station := range priceStations {
		priceEntryCount += len(station.FuelPrices)
	}
	priceStationsMutex.Unlock()

	stationLocationsMutex.Lock()
	stationLocationsCount := len(stationLocations)
	stationLocationsMutex.Unlock()

	fuelTypesCacheMutex.Lock()
	fuelTypesCount := len(fuelTypesCache)
	fuelTypesCacheMutex.Unlock()

	savedStationsPagesMutex.Lock()
	cachedStationPagesCount := len(savedStationsPages)
	oldestCachedAt := time.Time{}
	for _, cache := range savedStationsPages {
		if oldestCachedAt.IsZero() || cache.CreatedAt.Before(oldestCachedAt) {
			oldestCachedAt = cache.CreatedAt
		}
	}
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	cachedPricePagesCount := len(savedPricesPages)
	for _, cache := range savedPricesPages {
		if oldestCachedAt.IsZero() || cache.CreatedAt.Before(oldestCachedAt) {
			oldestCachedAt = cache.CreatedAt
		}
	}
	savedPricesPagesMutex.Unlock()

	oldestCachedAge := time.Duration(0)
	if !oldestCachedAt.IsZero() {
		oldestCachedAge = now.Sub(oldestCachedAt)
		if oldestCachedAge < 0 {
			oldestCachedAge = 0
		}
	}

	return memoryInfo{
		StationsCount:              stationsCount,
		PriceStationsCount:         priceStationsCount,
		StationPriceEntriesCount:   priceEntryCount,
		StationLocationsCount:      stationLocationsCount,
		FuelTypesCachedCount:       fuelTypesCount,
		CachedStationPagesCount:    cachedStationPagesCount,
		CachedPricePagesCount:      cachedPricePagesCount,
		OldestCachedPageAgeSeconds: int64(oldestCachedAge.Seconds()),
		OldestCachedPageAgeHuman:   oldestCachedAge.Round(time.Second).String(),
	}
}

func collectGovAPIStats(now time.Time) govAPIInfo {
	snapshot, ok := getGovAPIStatsSnapshot()
	if !ok {
		return govAPIInfo{StatsAvailable: false}
	}

	lifetime := now.Sub(snapshot.StartedAt)
	avgRPM := 0.0
	if lifetime > 0 {
		avgRPM = float64(snapshot.TotalRequests) / lifetime.Minutes()
	}

	percent := (avgRPM / 30.0) * 100.0
	if percent < 0 {
		percent = 0
	}

	return govAPIInfo{
		StatsAvailable:       true,
		StatsSince:           snapshot.StartedAt.UTC().Format(time.RFC3339),
		TotalRequests:        snapshot.TotalRequests,
		Requests2xx:          snapshot.Status2xx,
		Requests4xx:          snapshot.Status4xx,
		Requests5xx:          snapshot.Status5xx,
		Requests401:          snapshot.Status401,
		Requests403:          snapshot.Status403,
		NetworkErrors:        snapshot.NetworkErrors,
		InFlightRequests:     snapshot.InFlight,
		PeakInFlightRequests: snapshot.PeakInFlight,
		AvgRequestsPerMinute: avgRPM,
		PercentOf30RPMLimit:  percent,
	}
}

func collectTimerStats(now time.Time) timersInfo {
	enrichmentScheduled, enrichmentNext := getEnrichmentTimerSnapshot()

	cycleTimeMutex.RLock()
	lastPrices := lastPricesCycleComplete
	lastStations := lastStationsCycleComplete
	cycleTimeMutex.RUnlock()

	return timersInfo{
		Enrichment:            buildScheduledTimerInfo(enrichmentScheduled, enrichmentNext, now),
		PricesCycleCooldown:   buildCooldownInfo(lastPrices, 15*time.Minute, now),
		StationsCycleCooldown: buildCooldownInfo(lastStations, time.Hour, now),
	}
}

func buildScheduledTimerInfo(isScheduled bool, nextRunAt time.Time, now time.Time) scheduledTimerInfo {
	if !isScheduled || nextRunAt.IsZero() {
		return scheduledTimerInfo{IsScheduled: false}
	}

	remaining := nextRunAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}

	return scheduledTimerInfo{
		IsScheduled:         true,
		NextRunAt:           nextRunAt.UTC().Format(time.RFC3339),
		SecondsUntilNextRun: int64(remaining.Seconds()),
		HumanUntilNextRun:   remaining.Round(time.Second).String(),
	}
}

func buildCooldownInfo(lastCompletedAt time.Time, cooldown time.Duration, now time.Time) cooldownInfo {
	if lastCompletedAt.IsZero() {
		return cooldownInfo{CooldownDurationSeconds: int64(cooldown.Seconds())}
	}

	nextEligible := lastCompletedAt.Add(cooldown)
	remaining := nextEligible.Sub(now)
	inCooldown := remaining > 0
	if remaining < 0 {
		remaining = 0
	}

	return cooldownInfo{
		LastCompletedAt:             lastCompletedAt.UTC().Format(time.RFC3339),
		CooldownDurationSeconds:     int64(cooldown.Seconds()),
		InCooldown:                  inCooldown,
		NextEligibleRunAt:           nextEligible.UTC().Format(time.RFC3339),
		SecondsUntilNextEligibleRun: int64(remaining.Seconds()),
		HumanUntilNextEligibleRun:   remaining.Round(time.Second).String(),
	}
}

func collectRuntimeStats(now time.Time) runtimeInfo {
	uptime := now.Sub(runtimeStatsProcessStartedAt)
	if uptime < 0 {
		uptime = 0
	}

	var memStats goruntime.MemStats
	goruntime.ReadMemStats(&memStats)

	return runtimeInfo{
		RetryQueueLength:            globalRetryQueue.Len(),
		PricesMaxPagesPerCycleCap:   getDynamicMaxPagesPerCycle(false),
		StationsMaxPagesPerCycleCap: getDynamicMaxPagesPerCycle(true),
		ProcessStartedAt:            runtimeStatsProcessStartedAt.UTC().Format(time.RFC3339),
		ProcessUptimeSeconds:        int64(uptime.Seconds()),
		ProcessUptimeHuman:          uptime.Round(time.Second).String(),
		RAMSysBytes:                 memStats.Sys,
		RAMSysHuman:                 formatBytesHuman(memStats.Sys),
		RAMHeapAllocBytes:           memStats.HeapAlloc,
		RAMHeapAllocHuman:           formatBytesHuman(memStats.HeapAlloc),
		RAMNextGCBytes:              memStats.NextGC,
		RAMNextGCHuman:              formatBytesHuman(memStats.NextGC),
		RAMGCCycles:                 memStats.NumGC,
		RAMGCCyclesHuman:            formatCyclesHuman(memStats.NumGC),
	}
}

func formatBytesHuman(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div := float64(unit)
	exp := 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(bytes)/div, "KMGTPE"[exp])
}

func formatCyclesHuman(cycles uint32) string {
	return fmt.Sprintf("%d cycles", cycles)
}

func evaluateStatsHealth(disk diskCacheInfo, memory memoryInfo, gov govAPIInfo) healthInfo {
	reasons := make([]string, 0)

	if disk.JSONFileCount == 0 {
		reasons = append(reasons, "no_json_cache_files_found")
	} else if time.Duration(disk.OldestFileAgeSeconds)*time.Second > statsWarnOldestJSONFileAge {
		reasons = append(reasons, "oldest_json_file_is_stale")
	}

	if memory.CachedStationPagesCount+memory.CachedPricePagesCount == 0 {
		reasons = append(reasons, "no_cached_pages_in_memory")
	} else if time.Duration(memory.OldestCachedPageAgeSeconds)*time.Second > statsWarnOldestMemoryCacheAge {
		reasons = append(reasons, "oldest_cached_page_in_memory_is_stale")
	}

	if !gov.StatsAvailable {
		reasons = append(reasons, "gov_api_stats_unavailable")
	} else if gov.TotalRequests > 0 {
		requestTotal := float64(gov.TotalRequests)
		forbiddenRatio := float64(gov.Requests403) / requestTotal * 100
		clientErrorRatio := float64(gov.Requests4xx) / requestTotal * 100
		networkErrorRatio := float64(gov.NetworkErrors) / requestTotal * 100

		if forbiddenRatio >= statsWarn403RatioPercent {
			reasons = append(reasons, "high_403_ratio")
		}
		if clientErrorRatio >= statsWarn4xxRatioPercent {
			reasons = append(reasons, "high_4xx_ratio")
		}
		if networkErrorRatio >= statsWarnNetworkErrorRatioPercent {
			reasons = append(reasons, "high_network_error_ratio")
		}
	}

	if len(reasons) == 0 {
		return healthInfo{Status: "ok"}
	}

	sort.Strings(reasons)
	return healthInfo{Status: "warn", Reasons: reasons}
}
