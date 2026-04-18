package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

const (
	ukMinLatitude  = 49.5
	ukMaxLatitude  = 61.5
	ukMinLongitude = -11.0
	ukMaxLongitude = 1.9
)

type geoPoint struct {
	lat float32
	lng float32
}

// ukGeofencePolygons holds OSM-derived UK polygons used only for coordinate correction.
var ukGeofencePolygons = [][]geoPoint{}
var ukPolygonMutex sync.RWMutex
var ukPolygonLoaded = false
var ukBoundaryFilePaths = []string{
	"uk_land_osm.json",
	"app/uk_land_osm.json",
}
var ukBoundaryOnce sync.Once

// loadUKBoundary loads the OSM-derived UK polygons used for coordinate correction.
func loadUKBoundary() {
	ukPolygonMutex.Lock()
	defer ukPolygonMutex.Unlock()

	if ukPolygonLoaded {
		return
	}

	var data []byte
	var err error
	loadedPath := ""
	for _, candidatePath := range ukBoundaryFilePaths {
		data, err = os.ReadFile(candidatePath)
		if err == nil {
			loadedPath = candidatePath
			break
		}
	}
	if loadedPath == "" {
		log.Printf("[GEO] WARNING: Could not load OSM UK boundary file from any known path: %v", err)
		ukPolygonLoaded = true
		return
	}

	var polygons [][][]float64
	if err := json.Unmarshal(data, &polygons); err != nil {
		log.Printf("[GEO] WARNING: Could not parse OSM UK boundary JSON: %v", err)
		ukPolygonLoaded = true
		return
	}

	loadedPolygons := make([][]geoPoint, 0, len(polygons))
	for _, polygon := range polygons {
		converted := make([]geoPoint, 0, len(polygon))
		for _, point := range polygon {
			if len(point) >= 2 {
				converted = append(converted, geoPoint{lat: float32(point[0]), lng: float32(point[1])})
			}
		}
		if len(converted) >= 3 {
			loadedPolygons = append(loadedPolygons, converted)
		}
	}

	if len(loadedPolygons) == 0 {
		log.Printf("[GEO] WARNING: OSM UK boundary JSON did not contain any usable polygons")
		ukPolygonLoaded = true
		return
	}

	ukGeofencePolygons = loadedPolygons
	log.Printf("[GEO] Loaded OSM UK boundary with %d polygons from %s", len(ukGeofencePolygons), loadedPath)
	ukPolygonLoaded = true
}

func isWithinWorldBounds(lat, lng float32) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func isWithinUKBounds(lat, lng float32) bool {
	return lat >= float32(ukMinLatitude) && lat <= float32(ukMaxLatitude) && lng >= float32(ukMinLongitude) && lng <= float32(ukMaxLongitude)
}

func isPointInPolygon(lat, lng float32, polygon []geoPoint) bool {
	if len(polygon) < 3 {
		return false
	}

	inside := false
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		pi := polygon[i]
		pj := polygon[j]

		intersects := (pi.lat > lat) != (pj.lat > lat)
		if !intersects {
			continue
		}

		xAtLat := (pj.lng-pi.lng)*(lat-pi.lat)/(pj.lat-pi.lat) + pi.lng
		if lng < xAtLat {
			inside = !inside
		}
	}

	return inside
}

func isWithinUKGeofence(lat, lng float32) bool {
	ukBoundaryOnce.Do(loadUKBoundary)
	ukPolygonMutex.RLock()
	defer ukPolygonMutex.RUnlock()

	for _, polygon := range ukGeofencePolygons {
		if isPointInPolygon(lat, lng, polygon) {
			return true
		}
	}
	return false
}

func isValidUKCoordinate(lat, lng float32) bool {
	return isWithinUKBounds(lat, lng) && isWithinUKGeofence(lat, lng)
}

func hasUKGeofenceData() bool {
	ukBoundaryOnce.Do(loadUKBoundary)
	ukPolygonMutex.RLock()
	defer ukPolygonMutex.RUnlock()
	return len(ukGeofencePolygons) > 0
}

func normalizeUKStationCoordinates(lat, lng float64) (float64, float64, bool) {
	lat32 := float32(lat)
	lng32 := float32(lng)
	if !isWithinWorldBounds(lat32, lng32) {
		return 0, 0, false
	}

	// Try the original and common corruption variants.
	candidates := []geoPoint{
		{lat: lat32, lng: lng32},  // original
		{lat: lng32, lng: lat32},  // swapped
		{lat: lat32, lng: -lng32}, // longitude sign flipped
		{lat: lng32, lng: -lat32}, // swapped + longitude sign flipped in swapped form
	}

	for _, candidate := range candidates {
		if isValidUKCoordinate(candidate.lat, candidate.lng) {
			return float64(candidate.lat), float64(candidate.lng), true
		}
	}

	return 0, 0, false
}

func sanitizeStationsForUKMapView(input []Station) ([]Station, int, int) {
	sanitized := make([]Station, 0, len(input))
	fixed := 0
	dropped := 0

	for _, station := range input {
		lat := float64(station.Location.Latitude)
		lng := float64(station.Location.Longitude)

		normalizedLat, normalizedLng, ok := normalizeUKStationCoordinates(lat, lng)
		if !ok {
			dropped++
			continue
		}

		if normalizedLat != lat || normalizedLng != lng {
			fixed++
		}

		station.Location.Latitude = FlexFloat(normalizedLat)
		station.Location.Longitude = FlexFloat(normalizedLng)
		sanitized = append(sanitized, station)
	}

	return sanitized, fixed, dropped
}
