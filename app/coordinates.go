package main

const (
	ukMinLatitude  = 49.5
	ukMaxLatitude  = 61.5
	ukMinLongitude = -11.0
	ukMaxLongitude = 3.0
)

type geoPoint struct {
	lat float64
	lng float64
}

var ukGeofencePolygons = [][]geoPoint{
	// Simplified Great Britain coastline polygon.
	{
		{lat: 49.90, lng: -6.50},
		{lat: 50.00, lng: -5.20},
		{lat: 50.40, lng: -4.20},
		{lat: 50.60, lng: -3.50},
		{lat: 50.70, lng: -2.50},
		{lat: 50.80, lng: -1.50},
		{lat: 50.70, lng: -0.50},
		{lat: 50.70, lng: 0.30},
		{lat: 51.00, lng: 1.30},
		{lat: 52.00, lng: 1.80},
		{lat: 53.20, lng: 1.60},
		{lat: 54.50, lng: 0.80},
		{lat: 55.20, lng: -1.50},
		{lat: 55.80, lng: -2.10},
		{lat: 56.50, lng: -3.10},
		{lat: 57.30, lng: -3.30},
		{lat: 58.20, lng: -3.00},
		{lat: 58.80, lng: -2.70},
		{lat: 59.00, lng: -4.80},
		{lat: 58.50, lng: -6.20},
		{lat: 57.80, lng: -7.20},
		{lat: 56.80, lng: -7.60},
		{lat: 55.80, lng: -6.50},
		{lat: 55.00, lng: -5.60},
		{lat: 54.20, lng: -5.30},
		{lat: 53.40, lng: -4.70},
		{lat: 52.80, lng: -4.40},
		{lat: 52.10, lng: -5.20},
		{lat: 51.50, lng: -5.50},
		{lat: 50.90, lng: -5.90},
		{lat: 50.20, lng: -6.20},
	},
	// Simplified Northern Ireland polygon.
	{
		{lat: 54.35, lng: -8.20},
		{lat: 55.35, lng: -8.20},
		{lat: 55.35, lng: -5.30},
		{lat: 54.35, lng: -5.30},
	},
}

func isWithinWorldBounds(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func isWithinUKBounds(lat, lng float64) bool {
	return lat >= ukMinLatitude && lat <= ukMaxLatitude && lng >= ukMinLongitude && lng <= ukMaxLongitude
}

func isPointInPolygon(lat, lng float64, polygon []geoPoint) bool {
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

func isWithinUKGeofence(lat, lng float64) bool {
	for _, polygon := range ukGeofencePolygons {
		if isPointInPolygon(lat, lng, polygon) {
			return true
		}
	}

	return false
}

func isValidUKCoordinate(lat, lng float64) bool {
	return isWithinUKBounds(lat, lng) && isWithinUKGeofence(lat, lng)
}

func normalizeUKStationCoordinates(lat, lng float64) (float64, float64, bool) {
	if !isWithinWorldBounds(lat, lng) {
		return 0, 0, false
	}

	if isValidUKCoordinate(lat, lng) {
		return lat, lng, true
	}

	if isValidUKCoordinate(lng, lat) {
		return lng, lat, true
	}

	if isValidUKCoordinate(lat, -lng) {
		return lat, -lng, true
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
