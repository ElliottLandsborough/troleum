package main

import "testing"

func TestNormalizeUKStationCoordinates(t *testing.T) {
	tests := []struct {
		name        string
		lat         float64
		lng         float64
		wantLat     float64
		wantLng     float64
		shouldSucceed bool
	}{
		{
			name:          "valid uk coordinate unchanged",
			lat:           51.5074,
			lng:           -0.1278,
			wantLat:       51.5074,
			wantLng:       -0.1278,
			shouldSucceed: true,
		},
		{
			name:          "swapped coordinate fixed",
			lat:           -4.3215535,
			lng:           55.9174088,
			wantLat:       55.9174088,
			wantLng:       -4.3215535,
			shouldSucceed: true,
		},
		{
			name:          "longitude sign fixed",
			lat:           54.4629,
			lng:           6.5151,
			wantLat:       54.4629,
			wantLng:       -6.5151,
			shouldSucceed: true,
		},
		{
			name:          "unrecoverable coordinate rejected",
			lat:           42.258815,
			lng:           -0.288478,
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLat, gotLng, ok := normalizeUKStationCoordinates(tt.lat, tt.lng)
			if ok != tt.shouldSucceed {
				t.Fatalf("normalizeUKStationCoordinates() success = %v, want %v", ok, tt.shouldSucceed)
			}

			if !tt.shouldSucceed {
				return
			}

			if gotLat != tt.wantLat || gotLng != tt.wantLng {
				t.Fatalf("normalizeUKStationCoordinates() = (%v, %v), want (%v, %v)", gotLat, gotLng, tt.wantLat, tt.wantLng)
			}
		})
	}
}

func TestSanitizeStationsForUKMapView(t *testing.T) {
	input := []Station{
		{
			NodeID: "good",
			Location: Location{
				Latitude:  FlexFloat(51.5),
				Longitude: FlexFloat(-0.1),
			},
		},
		{
			NodeID: "swap",
			Location: Location{
				Latitude:  FlexFloat(-4.3215535),
				Longitude: FlexFloat(55.9174088),
			},
		},
		{
			NodeID: "drop",
			Location: Location{
				Latitude:  FlexFloat(42.258815),
				Longitude: FlexFloat(-0.288478),
			},
		},
	}

	sanitized, fixed, dropped := sanitizeStationsForUKMapView(input)

	if len(sanitized) != 2 {
		t.Fatalf("sanitizeStationsForUKMapView() len = %d, want 2", len(sanitized))
	}

	if fixed != 1 {
		t.Fatalf("sanitizeStationsForUKMapView() fixed = %d, want 1", fixed)
	}

	if dropped != 1 {
		t.Fatalf("sanitizeStationsForUKMapView() dropped = %d, want 1", dropped)
	}

	if sanitized[1].NodeID != "swap" {
		t.Fatalf("sanitizeStationsForUKMapView() second station = %s, want swap", sanitized[1].NodeID)
	}

	if float64(sanitized[1].Location.Latitude) != 55.9174088 || float64(sanitized[1].Location.Longitude) != -4.3215535 {
		t.Fatalf("sanitizeStationsForUKMapView() swap fix failed, got (%v, %v)", sanitized[1].Location.Latitude, sanitized[1].Location.Longitude)
	}
}