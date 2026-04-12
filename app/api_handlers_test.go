package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStationsAPIHandlerEdgeCases tests API parameter validation with insane/edge case values
func TestStationsAPIHandlerEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		shouldError    bool
		description    string
	}{
		// Valid cases
		{
			name:           "valid_lat_lng_no_filter",
			queryParams:    "lat=51.5074&lng=-0.1278",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Valid London coordinates",
		},
		{
			name:           "valid_with_fuel_type",
			queryParams:    "lat=51.5074&lng=-0.1278&fuel_type=E10",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Valid coordinates with valid fuel type",
		},
		{
			name:           "valid_bbox",
			queryParams:    "bbox=51.0,-0.5,52.0,0.5",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Valid bounding box",
		},
		{
			name:           "no_params",
			queryParams:    "",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "No parameters returns all stations",
		},

		// Insane lat/lng values
		{
			name:           "lat_way_too_high",
			queryParams:    "lat=999999.9999&lng=-0.1278",
			expectedStatus: http.StatusOK, // regex allows it, but coordinates will be invalid
			shouldError:    false,
			description:    "Latitude far beyond Earth bounds (999999)",
		},
		{
			name:           "lat_negative_way_too_high",
			queryParams:    "lat=-50000.5&lng=-0.1278",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Negative latitude beyond bounds",
		},
		{
			name:           "lng_way_too_high",
			queryParams:    "lat=51.5074&lng=999999.5",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Longitude far beyond Earth bounds",
		},
		{
			name:           "lat_infinity_string",
			queryParams:    "lat=Infinity&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Infinity as lat value",
		},
		{
			name:           "lat_nan",
			queryParams:    "lat=NaN&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "NaN as lat value",
		},
		{
			name:           "lat_with_letters",
			queryParams:    "lat=51.5074abc&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Latitude with letter suffix",
		},
		{
			name:           "lng_with_special_chars",
			queryParams:    "lat=51.5074&lng=-0.1278$$$",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Longitude with special characters",
		},
		{
			name:           "lat_multiple_decimal_points",
			queryParams:    "lat=51.50.74&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Latitude with multiple decimal points",
		},
		{
			name:           "lng_multiple_decimals",
			queryParams:    "lat=51.5074&lng=-0.12.78",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Longitude with multiple decimal points",
		},
		{
			name:           "lat_multiple_minus_signs",
			queryParams:    "lat=--51.5074&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Latitude with double minus sign",
		},
		{
			name:           "lat_plus_sign",
			queryParams:    "lat=+51.5074&lng=-0.1278",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Latitude with plus sign (not in regex)",
		},

		// Fuel type validation
		{
			name:           "fuel_type_lowercase",
			queryParams:    "fuel_type=e10",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Lowercase fuel type rejected",
		},
		{
			name:           "fuel_type_too_long",
			queryParams:    "fuel_type=VERYLONGFUELTYPENAMEOVER16CHARS",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type exceeds 16 char limit",
		},
		{
			name:           "fuel_type_with_space",
			queryParams:    "fuel_type=E10+PLUS",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type with space (+ decodes to space)",
		},
		{
			name:           "fuel_type_with_hyphen",
			queryParams:    "fuel_type=E-10",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type with hyphen (not allowed)",
		},
		{
			name:           "fuel_type_with_quote",
			queryParams:    "fuel_type=E10%27_DROP",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type with URL-encoded quote (injection attempt)",
		},
		{
			name:           "fuel_type_special_chars",
			queryParams:    "fuel_type=E10<!>",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type with special characters",
		},
		{
			name:           "fuel_type_valid_underscore",
			queryParams:    "fuel_type=E10_PLUS",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Fuel type with underscore (allowed)",
		},
		{
			name:           "fuel_type_valid_numbers",
			queryParams:    "fuel_type=E10_95",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Fuel type with numbers and underscore",
		},

		// Empty/missing parameters
		{
			name:           "lat_empty",
			queryParams:    "lat=&lng=-0.1278",
			expectedStatus: http.StatusOK, // empty string is valid (just not used)
			shouldError:    false,
			description:    "Empty lat parameter",
		},
		{
			name:           "lng_empty",
			queryParams:    "lat=51.5074&lng=",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Empty lng parameter",
		},
		{
			name:           "both_empty",
			queryParams:    "lat=&lng=",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Both lat and lng empty",
		},
		{
			name:           "fuel_type_empty",
			queryParams:    "fuel_type=",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Empty fuel_type parameter",
		},

		// Bounding box edge cases
		{
			name:           "bbox_missing_parts",
			queryParams:    "bbox=51.0,-0.5,52.0",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Bbox with only 3 parts",
		},
		{
			name:           "bbox_too_many_parts",
			queryParams:    "bbox=51.0,-0.5,52.0,0.5,extra",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Bbox with 5 parts",
		},
		{
			name:           "bbox_with_non_numeric",
			queryParams:    "bbox=abc,def,ghi,jkl",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Bbox with non-numeric values",
		},
		{
			name:           "bbox_infinity",
			queryParams:    "bbox=Infinity,-0.5,52.0,0.5",
			expectedStatus: http.StatusOK, // Infinity parses as valid float64, just doesn't filter usefully
			shouldError:    false,
			description:    "Bbox with Infinity value (parses but doesn't filter)",
		},
		{
			name:           "bbox_inverted_lat",
			queryParams:    "bbox=52.0,-0.5,51.0,0.5",
			expectedStatus: http.StatusOK, // No validation of min/max order
			shouldError:    false,
			description:    "Bbox with inverted latitude (minLat > maxLat)",
		},
		{
			name:           "bbox_inverted_lng",
			queryParams:    "bbox=51.0,0.5,52.0,-0.5",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Bbox with inverted longitude (minLng > maxLng)",
		},
		{
			name:           "bbox_with_extreme_values",
			queryParams:    "bbox=-90,-180,90,180",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Bbox covering entire world",
		},
		{
			name:           "bbox_beyond_world",
			queryParams:    "bbox=-999,-999,999,999",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Bbox with values beyond Earth bounds",
		},

		// Very long strings / buffer overflow attempts
		{
			name:           "fuel_type_very_long",
			queryParams:    "fuel_type=" + strings.Repeat("A", 1000),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type 1000 chars (way over limit)",
		},
		{
			name:           "lat_very_long_number",
			queryParams:    "lat=" + strings.Repeat("9", 500),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Latitude as 500-digit number (exceeds parameter length limit)",
		},
		{
			name:           "bbox_very_long_format",
			queryParams:    "bbox=" + strings.Repeat("1.5,", 100),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Bbox with 100+ comma-separated parts",
		},

		// URL encoding edge cases
		{
			name:           "lat_url_encoded_special",
			queryParams:    "lat=%3C%3E%00%01",
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Lat with URL-encoded special characters",
		},
		{
			name:           "fuel_type_unicode",
			queryParams:    "fuel_type=E10%C3%A9", // UTF-8 for "é"
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Fuel type with unicode character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stations?"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d. Query: %s", tt.description, tt.expectedStatus, w.Code, tt.queryParams)
			}

			if tt.shouldError && w.Code < 400 {
				t.Errorf("%s: expected error (4xx/5xx) but got %d", tt.description, w.Code)
			}
		})
	}
}

// Benchmark to ensure that parameter validation doesn't have significant performance impact
func BenchmarkStationsAPIHandlerValidation(b *testing.B) {
	cases := []struct {
		name  string
		query string
	}{
		{"valid_standard", "lat=51.5074&lng=-0.1278&fuel_type=E10"},
		{"no_params", ""},
		{"bbox", "bbox=51.0,-0.5,52.0,0.5"},
		{"invalid_lat", "lat=abc&lng=-0.1278"},
		{"long_fuel_type", "fuel_type=" + strings.Repeat("A", 100)},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest("GET", "/api/stations?"+bc.query, nil)
				w := httptest.NewRecorder()
				stationsAPIHandler(w, req)
			}
		})
	}
}

// TestFuelTypeRegex tests the fuel type pattern validation more rigorously
func TestFuelTypeRegex(t *testing.T) {
	tests := []struct {
		fuelType    string
		isValid     bool
		description string
	}{
		{"E10", true, "Standard E10"},
		{"DIESEL", true, "Standard DIESEL"},
		{"PREMIUM_PETROL", true, "PREMIUM_PETROL with underscore"},
		{"E85", true, "E85"},
		{"SP95_E10", true, "SP95_E10"},
		{"BP_ULTIMATE_99", true, "BP_ULTIMATE_99"},
		{"A", true, "Single character"},
		{strings.Repeat("A", 16), true, "Maximum 16 characters"},
		{strings.Repeat("A", 17), false, "Over 16 characters"},
		{"", false, "Empty (doesn't match regex; API allows via empty check before regex)"},
		{"e10", false, "Lowercase"},
		{"E-10", false, "Hyphen not allowed"},
		{"E 10", false, "Space not allowed"},
		{"E.10", false, "Dot not allowed"},
		{"E@10", false, "@ not allowed"},
		{"E/10", false, "/ not allowed"},
		{"E+10", false, "+ not allowed"},
		{"E!10", false, "! not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			matches := fuelTypePattern.MatchString(tt.fuelType)
			if matches != tt.isValid {
				t.Errorf("%q: expected %v, got %v. Description: %s", tt.fuelType, tt.isValid, matches, tt.description)
			}
		})
	}
}

// TestCoordinateParsingEdgeCases tests float parsing edge cases
func TestCoordinateParsingEdgeCases(t *testing.T) {
	tests := []struct {
		latStr      string
		lngStr      string
		shouldFail  bool
		description string
	}{
		{"51.5074", "-0.1278", false, "Normal valid coordinates"},
		{"0", "0", false, "Zero coordinates"},
		{"-90", "180", false, "Extreme valid Earth coordinates"},
		{"90.0", "-180.0", false, "Poles and dateline"},
		{".5", "-.5", false, "Leading decimal point"},
		{"5.", "-5.", false, "Trailing decimal point"},
		{"1e10", "2e10", true, "Scientific notation (should fail regex)"},
		{"+51.5", "-0.1", true, "Plus sign (should fail regex)"},
		{"51,5", "-0,1", true, "Comma as decimal separator"},
		{"51.5074e", "-0.1278", true, "Letter e (not valid scientific notation)"},
		{"NaN", "0", true, "NaN string"},
		{"Infinity", "0", true, "Infinity string"},
		{"-Infinity", "0", true, "Minus Infinity string"},
		{"null", "0", true, "null string"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stations?lat="+tt.latStr+"&lng="+tt.lngStr, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			isError := w.Code >= 400
			if isError != tt.shouldFail {
				expectedStatus := "success"
				if tt.shouldFail {
					expectedStatus = "error"
				}
				t.Errorf("%s: expected %s but got status %d", tt.description, expectedStatus, w.Code)
			}
		})
	}
}

// Helper test to verify no panic with extreme float values
func TestNoPanicWithExtremeValues(t *testing.T) {
	extremeValues := []string{
		"999999999.999999",
		"-999999999.999999",
		"0.0000000001",
		"-0.0000000001",
		strings.Repeat("9", 300),
		"-" + strings.Repeat("9", 300),
	}

	for _, val := range extremeValues {
		t.Run("lat_"+val[:min(20, len(val))], func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stations?lat="+val+"&lng=0", nil)
			w := httptest.NewRecorder()

			// Should not panic
			stationsAPIHandler(w, req)

			// Should return either OK or BadRequest, never 500
			if w.Code >= 500 {
				t.Errorf("Got 5xx status %d on extreme value", w.Code)
			}
		})
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBboxParameterValidation tests bounding box validation more thoroughly
func TestBboxParameterValidation(t *testing.T) {
	tests := []struct {
		bbox        string
		shouldFail  bool
		description string
	}{
		{"51.0,-0.5,52.0,0.5", false, "Valid bbox"},
		{"0,0,1,1", false, "Small bbox"},
		{"-90,-180,90,180", false, "World bbox"},
		{"90,180,91,181", false, "Out of bounds but parseable"},
		{"-999,-999,999,999", false, "Extreme out of bounds"},
		{"51.0,-0.5,52.0", true, "Missing one coordinate"},
		{"51.0,-0.5,52.0,0.5,extra", true, "Extra coordinate"},
		{"51.0,-0.5,52.0,abc", true, "Non-numeric value"},
		{"abc,def,ghi,jkl", true, "All non-numeric"},
		{"", false, "Empty bbox (treated as not provided)"},
		{"51.0,-0.5,,0.5", true, "Missing middle value"},
		{"51.0,-0.5,NaN,0.5", false, "NaN in bbox (parses as valid float64)"},
		{"51.0,-0.5,Infinity,0.5", false, "Infinity in bbox (parses as valid float64)"},
		{"51.0;-0.5;52.0;0.5", false, "Semicolon separator (treated as single malformed string, returns 200 since bbox param missing)"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			query := "bbox=" + tt.bbox
			req := httptest.NewRequest("GET", "/api/stations?"+query, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			isError := w.Code >= 400
			if isError != tt.shouldFail {
				expectedStatus := "valid"
				if tt.shouldFail {
					expectedStatus = "invalid"
				}
				t.Errorf("%s: expected %s but got status %d", tt.description, expectedStatus, w.Code)
			}
		})
	}
}

// TestQueryParameterLengthLimits tests that oversized query parameters are rejected
func TestQueryParameterLengthLimits(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		shouldError    bool
		description    string
	}{
		// Valid sizes
		{
			name:           "normal_params",
			queryParams:    "lat=51.5074&lng=-0.1278&fuel_type=E10",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Normal request within limits",
		},
		{
			name:           "single_max_valid_param",
			queryParams:    "lat=" + strings.Repeat("5", 19), // 19 chars: realistic max for a coordinate
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Single parameter at realistic max (19 chars)",
		},

		// Individual parameter too long (over 100 chars)
		{
			name:           "lat_param_too_long",
			queryParams:    "lat=" + strings.Repeat("5", 101), // 101 chars - exceeds MaxParameterValueLength
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "lat parameter 101 chars (exceeds 100 limit)",
		},
		{
			name:           "lng_param_too_long",
			queryParams:    "lng=" + strings.Repeat("5", 101),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "lng parameter 101 chars (exceeds 100 limit)",
		},
		{
			name:           "fuel_type_param_too_long",
			queryParams:    "fuel_type=" + strings.Repeat("A", 101),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "fuel_type parameter 101 chars (exceeds 100 limit)",
		},
		{
			name:           "bbox_param_too_long",
			queryParams:    "bbox=" + strings.Repeat("1.0,", 101), // Creates a very long bbox string
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "bbox parameter 404 chars (exceeds 100 limit)",
		},

		// Total query string too long (over 1000 chars)
		{
			name:           "total_query_string_too_long",
			queryParams:    "lat=" + strings.Repeat("5", 50) + "&lng=" + strings.Repeat("5", 50) + "&bbox=" + strings.Repeat("1.0,", 200), // Creates > 1000 char query string
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Total query string over 1000 chars",
		},

		// Edge cases at boundaries
		{
			name:           "param_exactly_100_chars",
			queryParams:    "lat=" + strings.Repeat("5", 100),
			expectedStatus: http.StatusOK, // Exactly at limit, should pass
			shouldError:    false,
			description:    "Parameter exactly 100 chars (at limit)",
		},
		{
			name:           "param_101_chars",
			queryParams:    "lat=" + strings.Repeat("5", 101),
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Parameter 101 chars (just over limit)",
		},

		// Multiple parameters within limit
		{
			name:           "multiple_params_within_limits",
			queryParams:    "lat=" + strings.Repeat("5", 50) + "&lng=" + strings.Repeat("5", 50) + "&fuel_type=E10",
			expectedStatus: http.StatusOK,
			shouldError:    false,
			description:    "Multiple parameters all within individual limit",
		},

		// Multiple long parameters that exceed total limit
		{
			name:           "multiple_params_exceed_total",
			queryParams:    strings.Repeat("param=value&", 90), // Creates ~1100 char query string
			expectedStatus: http.StatusBadRequest,
			shouldError:    true,
			description:    "Multiple parameters exceed 1000 char total limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stations?"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d. Query length: %d chars", tt.description, tt.expectedStatus, w.Code, len(tt.queryParams))
			}

			if tt.shouldError && w.Code < 400 {
				t.Errorf("%s: expected error (4xx/5xx) but got %d", tt.description, w.Code)
			}
		})
	}
}

// TestValidateQueryParametersDirectly tests the validateQueryParameters function directly
func TestValidateQueryParametersDirectly(t *testing.T) {
	tests := []struct {
		name          string
		queryParams   string
		shouldError   bool
		errorContains string
		description   string
	}{
		{
			name:        "valid_params",
			queryParams: "lat=51.5074&lng=-0.1278",
			shouldError: false,
			description: "Valid parameters",
		},
		{
			name:          "single_param_too_long",
			queryParams:   "lat=" + strings.Repeat("5", 150),
			shouldError:   true,
			errorContains: "lat",
			description:   "Single parameter exceeds limit",
		},
		{
			name:          "query_string_too_long",
			queryParams:   strings.Repeat("key=value&", 150),
			shouldError:   true,
			errorContains: "query string too long",
			description:   "Query string itself exceeds 1000 chars",
		},
		{
			name:        "empty_query",
			queryParams: "",
			shouldError: false,
			description: "Empty query string (valid)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stations?"+tt.queryParams, nil)
			err := validateQueryParameters(req)

			if (err != nil) != tt.shouldError {
				if tt.shouldError {
					t.Errorf("%s: expected error but got none", tt.description)
				} else {
					t.Errorf("%s: expected no error but got: %v", tt.description, err)
				}
			}

			if err != nil && tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("%s: error should contain %q but got: %v", tt.description, tt.errorContains, err)
			}
		})
	}
}

// TestCoordinateRangeValidation tests latitude and longitude range validation
func TestCoordinateRangeValidation(t *testing.T) {
	tests := []struct {
		name             string
		latQueryParam    string
		lngQueryParam    string
		shouldRejectLat  bool
		shouldRejectLng  bool
		description      string
		expectedHTTPCode int
	}{
		// Valid ranges
		{
			name:             "valid_uk_coords",
			latQueryParam:    "51.5074",
			lngQueryParam:    "-0.1278",
			shouldRejectLat:  false,
			shouldRejectLng:  false,
			description:      "Valid London coordinates",
			expectedHTTPCode: http.StatusOK,
		},
		{
			name:             "lat_at_north_pole",
			latQueryParam:    "90",
			lngQueryParam:    "0",
			shouldRejectLat:  false,
			shouldRejectLng:  false,
			description:      "Latitude at north pole (valid limit)",
			expectedHTTPCode: http.StatusOK,
		},
		{
			name:             "lat_at_south_pole",
			latQueryParam:    "-90",
			lngQueryParam:    "0",
			shouldRejectLat:  false,
			shouldRejectLng:  false,
			description:      "Latitude at south pole (valid limit)",
			expectedHTTPCode: http.StatusOK,
		},
		{
			name:             "lng_at_dateline",
			latQueryParam:    "0",
			lngQueryParam:    "180",
			shouldRejectLat:  false,
			shouldRejectLng:  false,
			description:      "Longitude at dateline (valid limit)",
			expectedHTTPCode: http.StatusOK,
		},
		{
			name:             "lng_at_negative_dateline",
			latQueryParam:    "0",
			lngQueryParam:    "-180",
			shouldRejectLat:  false,
			shouldRejectLng:  false,
			description:      "Longitude at negative dateline (valid limit)",
			expectedHTTPCode: http.StatusOK,
		},

		// Out of range
		{
			name:             "lat_over_90",
			latQueryParam:    "91",
			lngQueryParam:    "0",
			shouldRejectLat:  true,
			shouldRejectLng:  false,
			description:      "Latitude > 90 (beyond north pole)",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lat_under_minus_90",
			latQueryParam:    "-91",
			lngQueryParam:    "0",
			shouldRejectLat:  true,
			shouldRejectLng:  false,
			description:      "Latitude < -90 (beyond south pole)",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lng_over_180",
			latQueryParam:    "0",
			lngQueryParam:    "181",
			shouldRejectLat:  false,
			shouldRejectLng:  true,
			description:      "Longitude > 180 (beyond dateline)",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lng_under_minus_180",
			latQueryParam:    "0",
			lngQueryParam:    "-181",
			shouldRejectLat:  false,
			shouldRejectLng:  true,
			description:      "Longitude < -180 (beyond dateline)",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lat_way_out_of_range",
			latQueryParam:    "999999",
			lngQueryParam:    "0",
			shouldRejectLat:  true,
			shouldRejectLng:  false,
			description:      "Latitude way out of range (999999)",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lng_way_out_of_range",
			latQueryParam:    "0",
			lngQueryParam:    "999999",
			shouldRejectLat:  false,
			shouldRejectLng:  true,
			description:      "Longitude way out of range (999999)",
			expectedHTTPCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "lat=" + tt.latQueryParam + "&lng=" + tt.lngQueryParam
			req := httptest.NewRequest("GET", "/api/stations?"+query, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			if w.Code != tt.expectedHTTPCode {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedHTTPCode, w.Code)
			}
		})
	}
}

// TestBboxRangeValidation tests bounding box range validation
func TestBboxRangeValidation(t *testing.T) {
	tests := []struct {
		name             string
		bboxQueryParam   string
		shouldError      bool
		expectedHTTPCode int
		description      string
	}{
		// Valid bboxes
		{
			name:             "valid_bbox",
			bboxQueryParam:   "51.0,-0.5,52.0,0.5",
			shouldError:      false,
			expectedHTTPCode: http.StatusOK,
			description:      "Valid bounding box",
		},
		{
			name:             "world_bbox",
			bboxQueryParam:   "-90,-180,90,180",
			shouldError:      false,
			expectedHTTPCode: http.StatusOK,
			description:      "World-spanning bounding box",
		},
		{
			name:             "single_point_bbox",
			bboxQueryParam:   "51.0,-0.5,51.0,-0.5",
			shouldError:      false,
			expectedHTTPCode: http.StatusOK,
			description:      "Bounding box with same min/max (single point)",
		},

		// Invalid ranges (min >= max)
		{
			name:             "inverted_lat",
			bboxQueryParam:   "52.0,-0.5,51.0,0.5",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with inverted latitude (minLat > maxLat)",
		},
		{
			name:             "inverted_lng",
			bboxQueryParam:   "51.0,0.5,52.0,-0.5",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with inverted longitude (minLng > maxLng)",
		},

		// Out of Earth bounds
		{
			name:             "lat_beyond_north",
			bboxQueryParam:   "50,-0.5,91,0.5",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with maxLat > 90",
		},
		{
			name:             "lat_beyond_south",
			bboxQueryParam:   "-91,-0.5,50,0.5",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with minLat < -90",
		},
		{
			name:             "lng_beyond_east",
			bboxQueryParam:   "50,-0.5,51,181",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with maxLng > 180",
		},
		{
			name:             "lng_beyond_west",
			bboxQueryParam:   "50,-181,51,0.5",
			shouldError:      true,
			expectedHTTPCode: http.StatusBadRequest,
			description:      "Bounding box with minLng < -180",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "bbox=" + tt.bboxQueryParam
			req := httptest.NewRequest("GET", "/api/stations?"+query, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			if w.Code != tt.expectedHTTPCode {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedHTTPCode, w.Code)
			}

			if tt.shouldError && w.Code < 400 {
				t.Errorf("%s: expected error but got success", tt.description)
			}
		})
	}
}

// TestMultipleDecimalPointsRejected tests that coordinates with multiple decimal points are rejected
func TestMultipleDecimalPointsRejected(t *testing.T) {
	tests := []struct {
		name             string
		latQueryParam    string
		lngQueryParam    string
		description      string
		expectedHTTPCode int
	}{
		{
			name:             "lat_double_decimal",
			latQueryParam:    "51.5.074",
			lngQueryParam:    "-0.1278",
			description:      "Latitude with double decimal point",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lng_double_decimal",
			latQueryParam:    "51.5074",
			lngQueryParam:    "-0.12.78",
			description:      "Longitude with double decimal point",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "both_double_decimal",
			latQueryParam:    "51.5.074",
			lngQueryParam:    "-0.12.78",
			description:      "Both with double decimal points",
			expectedHTTPCode: http.StatusBadRequest,
		},
		{
			name:             "lat_triple_decimal",
			latQueryParam:    "51.5.0.74",
			lngQueryParam:    "-0.1278",
			description:      "Latitude with triple decimal point",
			expectedHTTPCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "lat=" + tt.latQueryParam + "&lng=" + tt.lngQueryParam
			req := httptest.NewRequest("GET", "/api/stations?"+query, nil)
			w := httptest.NewRecorder()

			stationsAPIHandler(w, req)

			if w.Code != tt.expectedHTTPCode {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedHTTPCode, w.Code)
			}
		})
	}
}

// TestIsValidLatitude tests the isValidLatitude helper function
func TestIsValidLatitude(t *testing.T) {
	tests := []struct {
		lat         float64
		isValid     bool
		description string
	}{
		{lat: 0, isValid: true, description: "Zero latitude (equator)"},
		{lat: 51.5, isValid: true, description: "Normal latitude"},
		{lat: 90, isValid: true, description: "North pole"},
		{lat: -90, isValid: true, description: "South pole"},
		{lat: -45.5, isValid: true, description: "Southern hemisphere"},
		{lat: 90.0001, isValid: false, description: "Just beyond north pole"},
		{lat: -90.0001, isValid: false, description: "Just beyond south pole"},
		{lat: 180, isValid: false, description: "Longitude value (should be invalid for latitude)"},
		{lat: 999999, isValid: false, description: "Way beyond north pole"},
		{lat: -999999, isValid: false, description: "Way beyond south pole"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := isValidLatitude(tt.lat)
			if result != tt.isValid {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.isValid, result)
			}
		})
	}
}

// TestIsValidLongitude tests the isValidLongitude helper function
func TestIsValidLongitude(t *testing.T) {
	tests := []struct {
		lng         float64
		isValid     bool
		description string
	}{
		{lng: 0, isValid: true, description: "Zero longitude (prime meridian)"},
		{lng: -0.1278, isValid: true, description: "Normal longitude"},
		{lng: 180, isValid: true, description: "Dateline (east)"},
		{lng: -180, isValid: true, description: "Dateline (west)"},
		{lng: 45.5, isValid: true, description: "Eastern hemisphere"},
		{lng: -120.5, isValid: true, description: "Western hemisphere"},
		{lng: 180.0001, isValid: false, description: "Just beyond dateline"},
		{lng: -180.0001, isValid: false, description: "Just beyond dateline (negative)"},
		{lng: 270, isValid: false, description: "Way beyond dateline"},
		{lng: -270, isValid: false, description: "Way beyond dateline (negative)"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := isValidLongitude(tt.lng)
			if result != tt.isValid {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.isValid, result)
			}
		})
	}
}
