package main

const (
	ukMinLatitude  = 49.5
	ukMaxLatitude  = 61.5
	ukMinLongitude = -11.0
	ukMaxLongitude = 3.0
)

func isWithinWorldBounds(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func isWithinUKBounds(lat, lng float64) bool {
	return lat >= ukMinLatitude && lat <= ukMaxLatitude && lng >= ukMinLongitude && lng <= ukMaxLongitude
}

func normalizeUKStationCoordinates(lat, lng float64) (float64, float64, bool) {
	if !isWithinWorldBounds(lat, lng) {
		return 0, 0, false
	}

	if isWithinUKBounds(lat, lng) {
		return lat, lng, true
	}

	if isWithinUKBounds(lng, lat) {
		return lng, lat, true
	}

	if isWithinUKBounds(lat, -lng) {
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
