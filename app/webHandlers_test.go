package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestStationsAPIHandlerEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		shouldError    bool
		description    string
	}{
		{name: "valid_lat_lng_no_filter", queryParams: "lat=51.5074&lng=-0.1278", expectedStatus: http.StatusOK, shouldError: false, description: "Valid London coordinates"},
		{name: "valid_with_fuel_type", queryParams: "lat=51.5074&lng=-0.1278&fuel_type=E10", expectedStatus: http.StatusOK, shouldError: false, description: "Valid coordinates with valid fuel type"},
		{name: "valid_bbox", queryParams: "bbox=51.0,-0.5,52.0,0.5", expectedStatus: http.StatusOK, shouldError: false, description: "Valid bounding box"},
		{name: "no_params", queryParams: "", expectedStatus: http.StatusOK, shouldError: false, description: "No parameters returns all stations"},
		{name: "lat_way_too_high", queryParams: "lat=999999.9999&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude far beyond Earth bounds (999999)"},
		{name: "lat_negative_way_too_high", queryParams: "lat=-50000.5&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Negative latitude beyond bounds"},
		{name: "lng_way_too_high", queryParams: "lat=51.5074&lng=999999.5", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Longitude far beyond Earth bounds"},
		{name: "lat_infinity_string", queryParams: "lat=Infinity&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Infinity as lat value"},
		{name: "lat_nan", queryParams: "lat=NaN&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "NaN as lat value"},
		{name: "lat_with_letters", queryParams: "lat=51.5074abc&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude with letter suffix"},
		{name: "lng_with_special_chars", queryParams: "lat=51.5074&lng=-0.1278$$$", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Longitude with special characters"},
		{name: "lat_multiple_decimal_points", queryParams: "lat=51.50.74&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude with multiple decimal points"},
		{name: "lng_multiple_decimals", queryParams: "lat=51.5074&lng=-0.12.78", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Longitude with multiple decimal points"},
		{name: "lat_multiple_minus_signs", queryParams: "lat=--51.5074&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude with double minus sign"},
		{name: "lat_plus_sign", queryParams: "lat=+51.5074&lng=-0.1278", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude with plus sign (not in regex)"},
		{name: "fuel_type_lowercase", queryParams: "fuel_type=e10", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Lowercase fuel type rejected"},
		{name: "fuel_type_too_long", queryParams: "fuel_type=VERYLONGFUELTYPENAMEOVER16CHARS", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type exceeds 16 char limit"},
		{name: "fuel_type_with_space", queryParams: "fuel_type=E10+PLUS", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type with space (+ decodes to space)"},
		{name: "fuel_type_with_hyphen", queryParams: "fuel_type=E-10", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type with hyphen (not allowed)"},
		{name: "fuel_type_with_quote", queryParams: "fuel_type=E10%27_DROP", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type with URL-encoded quote (injection attempt)"},
		{name: "fuel_type_special_chars", queryParams: "fuel_type=E10<!>", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type with special characters"},
		{name: "fuel_type_valid_underscore", queryParams: "fuel_type=E10_PLUS", expectedStatus: http.StatusOK, shouldError: false, description: "Fuel type with underscore (allowed)"},
		{name: "fuel_type_valid_numbers", queryParams: "fuel_type=E10_95", expectedStatus: http.StatusOK, shouldError: false, description: "Fuel type with numbers and underscore"},
		{name: "lat_empty", queryParams: "lat=&lng=-0.1278", expectedStatus: http.StatusOK, shouldError: false, description: "Empty lat parameter"},
		{name: "lng_empty", queryParams: "lat=51.5074&lng=", expectedStatus: http.StatusOK, shouldError: false, description: "Empty lng parameter"},
		{name: "both_empty", queryParams: "lat=&lng=", expectedStatus: http.StatusOK, shouldError: false, description: "Both lat and lng empty"},
		{name: "fuel_type_empty", queryParams: "fuel_type=", expectedStatus: http.StatusOK, shouldError: false, description: "Empty fuel_type parameter"},
		{name: "bbox_missing_parts", queryParams: "bbox=51.0,-0.5,52.0", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with only 3 parts"},
		{name: "bbox_too_many_parts", queryParams: "bbox=51.0,-0.5,52.0,0.5,extra", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with 5 parts"},
		{name: "bbox_with_non_numeric", queryParams: "bbox=abc,def,ghi,jkl", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with non-numeric values"},
		{name: "bbox_infinity", queryParams: "bbox=Infinity,-0.5,52.0,0.5", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with Infinity value"},
		{name: "bbox_inverted_lat", queryParams: "bbox=52.0,-0.5,51.0,0.5", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with inverted latitude (minLat > maxLat)"},
		{name: "bbox_inverted_lng", queryParams: "bbox=51.0,0.5,52.0,-0.5", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with inverted longitude (minLng > maxLng)"},
		{name: "bbox_with_extreme_values", queryParams: "bbox=-90,-180,90,180", expectedStatus: http.StatusOK, shouldError: false, description: "Bbox covering entire world"},
		{name: "bbox_beyond_world", queryParams: "bbox=-999,-999,999,999", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with values beyond Earth bounds"},
		{name: "fuel_type_very_long", queryParams: "fuel_type=" + strings.Repeat("A", 1000), expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type 1000 chars (way over limit)"},
		{name: "lat_very_long_number", queryParams: "lat=" + strings.Repeat("9", 500), expectedStatus: http.StatusBadRequest, shouldError: true, description: "Latitude as 500-digit number (exceeds parameter length limit)"},
		{name: "bbox_very_long_format", queryParams: "bbox=" + strings.Repeat("1.5,", 100), expectedStatus: http.StatusBadRequest, shouldError: true, description: "Bbox with 100+ comma-separated parts"},
		{name: "lat_url_encoded_special", queryParams: "lat=%3C%3E%00%01", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Lat with URL-encoded special characters"},
		{name: "fuel_type_unicode", queryParams: "fuel_type=E10%C3%A9", expectedStatus: http.StatusBadRequest, shouldError: true, description: "Fuel type with unicode character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+tt.queryParams, nil)
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

func BenchmarkStationsAPIHandlerValidation(b *testing.B) {
	cases := []struct{ name, query string }{
		{"valid_standard", "lat=51.5074&lng=-0.1278&fuel_type=E10"},
		{"no_params", ""},
		{"bbox", "bbox=51.0,-0.5,52.0,0.5"},
		{"invalid_lat", "lat=abc&lng=-0.1278"},
		{"long_fuel_type", "fuel_type=" + strings.Repeat("A", 100)},
	}
	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, "/api/stations?"+bc.query, nil)
				w := httptest.NewRecorder()
				stationsAPIHandler(w, req)
			}
		})
	}
}

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
		{".5", "-.5", true, "Leading decimal point"},
		{"5.", "-5.", true, "Trailing decimal point"},
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
			req := httptest.NewRequest(http.MethodGet, "/api/stations?lat="+tt.latStr+"&lng="+tt.lngStr, nil)
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

func TestNoPanicWithExtremeValues(t *testing.T) {
	extremeValues := []string{"999999999.999999", "-999999999.999999", "0.0000000001", "-0.0000000001", strings.Repeat("9", 300), "-" + strings.Repeat("9", 300)}
	for _, val := range extremeValues {
		t.Run("lat_"+val[:min(20, len(val))], func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stations?lat="+val+"&lng=0", nil)
			w := httptest.NewRecorder()
			stationsAPIHandler(w, req)
			if w.Code >= 500 {
				t.Errorf("Got 5xx status %d on extreme value", w.Code)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBboxParameterValidation(t *testing.T) {
	tests := []struct {
		bbox        string
		shouldFail  bool
		description string
	}{
		{"51.0,-0.5,52.0,0.5", false, "Valid bbox"},
		{"0,0,1,1", false, "Small bbox"},
		{"-90,-180,90,180", false, "World bbox"},
		{"90,180,91,181", true, "Out of bounds"},
		{"-999,-999,999,999", true, "Extreme out of bounds"},
		{"51.0,-0.5,52.0", true, "Missing one coordinate"},
		{"51.0,-0.5,52.0,0.5,extra", true, "Extra coordinate"},
		{"51.0,-0.5,52.0,abc", true, "Non-numeric value"},
		{"abc,def,ghi,jkl", true, "All non-numeric"},
		{"", false, "Empty bbox (treated as not provided)"},
		{"51.0,-0.5,,0.5", true, "Missing middle value"},
		{"51.0,-0.5,NaN,0.5", true, "NaN in bbox"},
		{"51.0,-0.5,Infinity,0.5", true, "Infinity in bbox"},
		{"51.0;-0.5;52.0;0.5", false, "Semicolon separator (treated as single malformed string, returns 200 since bbox param missing)"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			query := "bbox=" + tt.bbox
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+query, nil)
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

func TestQueryParameterLengthLimits(t *testing.T) {
	tests := []struct {
		name, queryParams string
		expectedStatus    int
		shouldError       bool
		description       string
	}{
		{"normal_params", "lat=51.5074&lng=-0.1278&fuel_type=E10", http.StatusOK, false, "Normal request within limits"},
		{"single_max_valid_param", "lat=" + strings.Repeat("5", 19), http.StatusOK, false, "Single parameter at realistic max (19 chars)"},
		{"lat_param_too_long", "lat=" + strings.Repeat("5", 101), http.StatusBadRequest, true, "lat parameter 101 chars (exceeds 100 limit)"},
		{"lng_param_too_long", "lng=" + strings.Repeat("5", 101), http.StatusBadRequest, true, "lng parameter 101 chars (exceeds 100 limit)"},
		{"fuel_type_param_too_long", "fuel_type=" + strings.Repeat("A", 101), http.StatusBadRequest, true, "fuel_type parameter 101 chars (exceeds 100 limit)"},
		{"bbox_param_too_long", "bbox=" + strings.Repeat("1.0,", 101), http.StatusBadRequest, true, "bbox parameter 404 chars (exceeds 100 limit)"},
		{"total_query_string_too_long", "lat=" + strings.Repeat("5", 50) + "&lng=" + strings.Repeat("5", 50) + "&bbox=" + strings.Repeat("1.0,", 200), http.StatusBadRequest, true, "Total query string over 1000 chars"},
		{"param_exactly_100_chars", "lat=" + strings.Repeat("5", 100), http.StatusOK, false, "Parameter exactly 100 chars (at limit)"},
		{"param_101_chars", "lat=" + strings.Repeat("5", 101), http.StatusBadRequest, true, "Parameter 101 chars (just over limit)"},
		{"multiple_params_within_limits", "lat=51.5074&lng=-0.1278&fuel_type=E10&client_id=" + strings.Repeat("A", 50), http.StatusOK, false, "Multiple parameters all within individual limit"},
		{"multiple_params_exceed_total", strings.Repeat("param=value&", 90), http.StatusBadRequest, true, "Multiple parameters exceed 1000 char total limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+tt.queryParams, nil)
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

func TestValidateQueryParametersDirectly(t *testing.T) {
	tests := []struct {
		name, queryParams string
		shouldError       bool
		errorContains     string
		description       string
	}{
		{"valid_params", "lat=51.5074&lng=-0.1278", false, "", "Valid parameters"},
		{"single_param_too_long", "lat=" + strings.Repeat("5", 150), true, "lat", "Single parameter exceeds limit"},
		{"query_string_too_long", strings.Repeat("key=value&", 150), true, "query string too long", "Query string itself exceeds 1000 chars"},
		{"empty_query", "", false, "", "Empty query string (valid)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+tt.queryParams, nil)
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

func TestCoordinateRangeValidation(t *testing.T) {
	tests := []struct {
		name, latQueryParam, lngQueryParam, description string
		expectedHTTPCode                                int
	}{
		{"valid_uk_coords", "51.5074", "-0.1278", "Valid London coordinates", http.StatusOK},
		{"lat_at_north_pole", "90", "0", "Latitude at north pole (valid limit)", http.StatusOK},
		{"lat_at_south_pole", "-90", "0", "Latitude at south pole (valid limit)", http.StatusOK},
		{"lng_at_dateline", "0", "180", "Longitude at dateline (valid limit)", http.StatusOK},
		{"lng_at_negative_dateline", "0", "-180", "Longitude at negative dateline (valid limit)", http.StatusOK},
		{"lat_over_90", "91", "0", "Latitude > 90 (beyond north pole)", http.StatusBadRequest},
		{"lat_under_minus_90", "-91", "0", "Latitude < -90 (beyond south pole)", http.StatusBadRequest},
		{"lng_over_180", "0", "181", "Longitude > 180 (beyond dateline)", http.StatusBadRequest},
		{"lng_under_minus_180", "0", "-181", "Longitude < -180 (beyond dateline)", http.StatusBadRequest},
		{"lat_way_out_of_range", "999999", "0", "Latitude way out of range (999999)", http.StatusBadRequest},
		{"lng_way_out_of_range", "0", "999999", "Longitude way out of range (999999)", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "lat=" + tt.latQueryParam + "&lng=" + tt.lngQueryParam
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+query, nil)
			w := httptest.NewRecorder()
			stationsAPIHandler(w, req)
			if w.Code != tt.expectedHTTPCode {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedHTTPCode, w.Code)
			}
		})
	}
}

func TestBboxRangeValidation(t *testing.T) {
	tests := []struct {
		name, bboxQueryParam, description string
		shouldError                       bool
		expectedHTTPCode                  int
	}{
		{"valid_bbox", "51.0,-0.5,52.0,0.5", "Valid bounding box", false, http.StatusOK},
		{"world_bbox", "-90,-180,90,180", "World-spanning bounding box", false, http.StatusOK},
		{"single_point_bbox", "51.0,-0.5,51.0,-0.5", "Bounding box with same min/max (single point)", false, http.StatusOK},
		{"inverted_lat", "52.0,-0.5,51.0,0.5", "Bounding box with inverted latitude (minLat > maxLat)", true, http.StatusBadRequest},
		{"inverted_lng", "51.0,0.5,52.0,-0.5", "Bounding box with inverted longitude (minLng > maxLng)", true, http.StatusBadRequest},
		{"lat_beyond_north", "50,-0.5,91,0.5", "Bounding box with maxLat > 90", true, http.StatusBadRequest},
		{"lat_beyond_south", "-91,-0.5,50,0.5", "Bounding box with minLat < -90", true, http.StatusBadRequest},
		{"lng_beyond_east", "50,-0.5,51,181", "Bounding box with maxLng > 180", true, http.StatusBadRequest},
		{"lng_beyond_west", "50,-181,51,0.5", "Bounding box with minLng < -180", true, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "bbox=" + tt.bboxQueryParam
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+query, nil)
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

func TestMultipleDecimalPointsRejected(t *testing.T) {
	tests := []struct {
		name, latQueryParam, lngQueryParam, description string
		expectedHTTPCode                                int
	}{
		{"lat_double_decimal", "51.5.074", "-0.1278", "Latitude with double decimal point", http.StatusBadRequest},
		{"lng_double_decimal", "51.5074", "-0.12.78", "Longitude with double decimal point", http.StatusBadRequest},
		{"both_double_decimal", "51.5.074", "-0.12.78", "Both with double decimal points", http.StatusBadRequest},
		{"lat_triple_decimal", "51.5.0.74", "-0.1278", "Latitude with triple decimal point", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "lat=" + tt.latQueryParam + "&lng=" + tt.lngQueryParam
			req := httptest.NewRequest(http.MethodGet, "/api/stations?"+query, nil)
			w := httptest.NewRecorder()
			stationsAPIHandler(w, req)
			if w.Code != tt.expectedHTTPCode {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedHTTPCode, w.Code)
			}
		})
	}
}

func TestIsValidLatitude(t *testing.T) {
	tests := []struct {
		lat         float64
		isValid     bool
		description string
	}{
		{0, true, "Zero latitude (equator)"}, {51.5, true, "Normal latitude"}, {90, true, "North pole"}, {-90, true, "South pole"}, {-45.5, true, "Southern hemisphere"}, {90.0001, false, "Just beyond north pole"}, {-90.0001, false, "Just beyond south pole"}, {180, false, "Longitude value (should be invalid for latitude)"}, {999999, false, "Way beyond north pole"}, {-999999, false, "Way beyond south pole"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if result := isValidLatitude(tt.lat); result != tt.isValid {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.isValid, result)
			}
		})
	}
}

func TestIsValidLongitude(t *testing.T) {
	tests := []struct {
		lng         float64
		isValid     bool
		description string
	}{
		{0, true, "Zero longitude (prime meridian)"}, {-0.1278, true, "Normal longitude"}, {180, true, "Dateline (east)"}, {-180, true, "Dateline (west)"}, {45.5, true, "Eastern hemisphere"}, {-120.5, true, "Western hemisphere"}, {180.0001, false, "Just beyond dateline"}, {-180.0001, false, "Just beyond dateline (negative)"}, {270, false, "Way beyond dateline"}, {-270, false, "Way beyond dateline (negative)"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if result := isValidLongitude(tt.lng); result != tt.isValid {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.isValid, result)
			}
		})
	}
}

func TestIsValidLatLngRejectsNaNAndInfinity(t *testing.T) {
	if isValidLatitude(math.NaN()) {
		t.Fatal("expected NaN latitude to be invalid")
	}
	if isValidLatitude(math.Inf(1)) {
		t.Fatal("expected +Inf latitude to be invalid")
	}
	if isValidLongitude(math.NaN()) {
		t.Fatal("expected NaN longitude to be invalid")
	}
	if isValidLongitude(math.Inf(-1)) {
		t.Fatal("expected -Inf longitude to be invalid")
	}
}

func TestFilterStationsByBoundingBox(t *testing.T) {
	stationsInput := []Station{{NodeID: "inside", Location: Location{Latitude: 51.5, Longitude: -0.1}}, {NodeID: "boundary", Location: Location{Latitude: 52.0, Longitude: 0.5}}, {NodeID: "outside-lat", Location: Location{Latitude: 53.0, Longitude: 0.0}}, {NodeID: "outside-lng", Location: Location{Latitude: 51.7, Longitude: 1.5}}}
	filtered := filterStationsByBoundingBox(stationsInput, 51.0, -0.5, 52.0, 0.5)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 stations in bbox, got %d", len(filtered))
	}
	if filtered[0].NodeID != "inside" || filtered[1].NodeID != "boundary" {
		t.Fatalf("unexpected filtered order/content: %#v", filtered)
	}
}

func TestSelectFirstStations(t *testing.T) {
	stationsInput := []Station{{NodeID: "1"}, {NodeID: "2"}, {NodeID: "3"}}
	selected := selectFirstStations(stationsInput, 2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 stations, got %d", len(selected))
	}
	if selected[0].NodeID != "1" || selected[1].NodeID != "2" {
		t.Fatalf("unexpected selected stations: %#v", selected)
	}
	all := selectFirstStations(stationsInput, 10)
	if len(all) != 3 {
		t.Fatalf("expected original slice when n exceeds len, got %d", len(all))
	}
}

func TestBuildLowestStationPriceIndex(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	priceStationsMutex.Lock()
	priceStations = []PriceStation{{NodeID: "station-1", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 155.0}, {FuelType: "DIESEL", Price: 149.9}}}, {NodeID: "station-2", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 0}, {FuelType: "DIESEL", Price: 160.0}}}, {NodeID: "station-3", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 0}}}}
	priceStationsIndex = map[string]int{"station-1": 0, "station-2": 1, "station-3": 2}
	priceStationsMutex.Unlock()
	lowest := buildLowestStationPriceIndex()
	if len(lowest) != 2 {
		t.Fatalf("expected 2 stations with valid prices, got %d", len(lowest))
	}
	if lowest["station-1"] != 149.9 {
		t.Fatalf("expected station-1 lowest price 149.9, got %v", lowest["station-1"])
	}
	if lowest["station-2"] != 160.0 {
		t.Fatalf("expected station-2 lowest price 160.0, got %v", lowest["station-2"])
	}
	if _, exists := lowest["station-3"]; exists {
		t.Fatal("expected station-3 to be excluded because it has no positive prices")
	}
}

func TestSelectStationsForBoundingBoxPrefersCheaperSameBucket(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	stationsInput := []Station{{NodeID: "a", Location: Location{Latitude: 1.0, Longitude: 1.0}}, {NodeID: "b", Location: Location{Latitude: 1.0, Longitude: 1.0}}, {NodeID: "c", Location: Location{Latitude: 1.0, Longitude: 1.0}}}
	priceStationsMutex.Lock()
	priceStations = []PriceStation{{NodeID: "a", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 155.0}}}, {NodeID: "b", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 145.0}}}, {NodeID: "c", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 135.0}}}}
	priceStationsIndex = map[string]int{"a": 0, "b": 1, "c": 2}
	priceStationsMutex.Unlock()
	selected := selectStationsForBoundingBox(stationsInput, 2, 0, 0, 10, 10)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected stations, got %d", len(selected))
	}
	if selected[0].NodeID != "c" || selected[1].NodeID != "b" {
		t.Fatalf("expected cheapest stations c,b in order, got %s,%s", selected[0].NodeID, selected[1].NodeID)
	}
}

func TestFormatStationName(t *testing.T) {
	tests := []struct {
		name    string
		station Station
		want    string
	}{
		{"unnamed", Station{}, "Unnamed Station"},
		{"same trading and brand", Station{BrandName: "Shell", TradingName: "Shell", IsSameTradingAndBrandName: true}, "Shell"},
		{"brand only", Station{BrandName: "Esso"}, "Esso"},
		{"trading only", Station{TradingName: "Local Fuel"}, "Local Fuel"},
		{"brand and trading", Station{BrandName: "BP", TradingName: "High Street"}, "BP - High Street"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatStationName(tt.station); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGetStationPricesReturnsCopy(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	priceStationsMutex.Lock()
	priceStations = []PriceStation{{NodeID: "station-1", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 150.0}}}}
	priceStationsIndex = map[string]int{"station-1": 0}
	priceStationsMutex.Unlock()
	prices := getStationPrices(Station{NodeID: "station-1"})
	if len(prices) != 1 {
		t.Fatalf("expected 1 price, got %d", len(prices))
	}
	prices[0].Price = 999.0
	priceStationsMutex.Lock()
	if priceStations[0].FuelPrices[0].Price != 150.0 {
		priceStationsMutex.Unlock()
		t.Fatal("expected returned prices to be a copy, but shared slice was modified")
	}
	priceStationsMutex.Unlock()
	if getStationPrices(Station{NodeID: "missing"}) != nil {
		t.Fatal("expected nil prices for missing station")
	}
}

func TestFuelTypesAPIHandler(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	fuelTypesCacheMutex.Lock()
	fuelTypesCache = []string{"E10", "DIESEL"}
	fuelTypesCacheMutex.Unlock()
	req := httptest.NewRequest(http.MethodGet, "/api/fuel-types", nil)
	w := httptest.NewRecorder()
	fuelTypesAPIHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content type, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "E10") || !strings.Contains(w.Body.String(), "DIESEL") {
		t.Fatalf("expected fuel types in body, got %s", w.Body.String())
	}
}

func TestStationsAPIHandlerLimitsTo100WithoutBbox(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	stationsMutex.Lock()
	stations = make([]Station, 0, 120)
	for i := 0; i < 120; i++ {
		stations = append(stations, Station{NodeID: "s" + strconv.Itoa(i), Location: Location{Latitude: 51.5, Longitude: -0.1}})
	}
	stationsMutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/stations", nil)
	w := httptest.NewRecorder()
	stationsAPIHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	entries, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected response data slice, got %T", resp.Data)
	}
	if len(entries) != 100 {
		t.Fatalf("expected 100 stations returned, got %d", len(entries))
	}
}

func TestStationsAPIHandlerLimitsTo100WithBbox(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	stationsMutex.Lock()
	stations = make([]Station, 0, 120)
	for i := 0; i < 120; i++ {
		stations = append(stations, Station{NodeID: "b" + strconv.Itoa(i), Location: Location{Latitude: 51.5, Longitude: -0.1}})
	}
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStations = make([]PriceStation, 0, 120)
	priceStationsIndex = make(map[string]int, 120)
	for i := 0; i < 120; i++ {
		nodeID := "b" + strconv.Itoa(i)
		priceStationsIndex[nodeID] = len(priceStations)
		priceStations = append(priceStations, PriceStation{NodeID: nodeID, FuelPrices: []FuelPrice{{FuelType: "E10", Price: float64(100 + i)}}})
	}
	priceStationsMutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/stations?bbox=51.0,-0.5,52.0,0.5", nil)
	w := httptest.NewRecorder()
	stationsAPIHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	entries, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected response data slice, got %T", resp.Data)
	}
	if len(entries) != 100 {
		t.Fatalf("expected 100 stations returned, got %d", len(entries))
	}
}
