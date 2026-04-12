package main

import "testing"

func TestNormalizeUKStationCoordinates(t *testing.T) {
	tests := []struct {
		name          string
		lat           float64
		lng           float64
		wantLat       float64
		wantLng       float64
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
			name:          "powick longitude sign fixed",
			lat:           52.163258,
			lng:           2.245994,
			wantLat:       52.163258,
			wantLng:       -2.245994,
			shouldSucceed: true,
		},
		{
			name:          "valid east coast positive longitude remains unchanged",
			lat:           52.62165,
			lng:           1.2966,
			wantLat:       52.62165,
			wantLng:       1.2966,
			shouldSucceed: true,
		},
		{
			name:          "whitby sign corrected",
			lat:           54.487166,
			lng:           0.624071,
			wantLat:       54.487166,
			wantLng:       -0.624071,
			shouldSucceed: true,
		},
		{
			name:          "mansfield sign corrected",
			lat:           53.1421,
			lng:           1.2067,
			wantLat:       53.1421,
			wantLng:       -1.2067,
			shouldSucceed: true,
		},
		{
			name:          "bawdrip belgium-side sign corrected",
			lat:           51.1558,
			lng:           2.9341,
			wantLat:       51.1558,
			wantLng:       -2.9341,
			shouldSucceed: true,
		},
		{
			name:          "bawdrip swapped and sign corrected",
			lat:           2.9341,
			lng:           51.1558,
			wantLat:       51.1558,
			wantLng:       -2.9341,
			shouldSucceed: true,
		},
		{
			name:          "holton heath sign corrected",
			lat:           50.7234,
			lng:           2.079,
			wantLat:       50.7234,
			wantLng:       -2.079,
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

func TestIsWithinUKGeofence(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		lng  float64
		want bool
	}{
		{
			name: "norwich in geofence",
			lat:  52.62165,
			lng:  1.2966,
			want: true,
		},
		{
			name: "powick bad sign outside geofence",
			lat:  52.163258,
			lng:  2.245994,
			want: false,
		},
		{
			name: "whitby positive longitude outside geofence",
			lat:  54.487166,
			lng:  0.624071,
			want: false,
		},
		{
			name: "whitby corrected longitude inside geofence",
			lat:  54.487166,
			lng:  -0.624071,
			want: true,
		},
		{
			name: "mansfield erroneous positive longitude outside geofence",
			lat:  53.1421,
			lng:  1.2067,
			want: false,
		},
		{
			name: "mansfield corrected negative longitude inside geofence",
			lat:  53.1421,
			lng:  -1.2067,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWithinUKGeofence(tt.lat, tt.lng)
			if got != tt.want {
				t.Fatalf("isWithinUKGeofence() = %v, want %v", got, tt.want)
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
