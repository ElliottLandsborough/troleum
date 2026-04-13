// a router or web handler or server that can return database data in a human readable format online
package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var fuelTypePattern = regexp.MustCompile(`^[A-Z0-9_]{1,16}$`)
var browserPhonePattern = regexp.MustCompile(`^[0-9+() /.-]{3,32}$`)

// Parameter size limits to prevent abuse
const (
	MaxQueryStringLength    = 1000 // Max total query string length (chars)
	MaxParameterValueLength = 100  // Max length for individual parameter values (chars)
)

// Coordinate bounds (Earth bounds)
const (
	MinLatitude  = -90.0
	MaxLatitude  = 90.0
	MinLongitude = -180.0
	MaxLongitude = 180.0
)

// APIResponse is the common JSON envelope for API responses.
type APIResponse struct {
	Code int `json:"code"`
	//Message string      `json:"message"`
	Data interface{} `json:"data,omitempty"`
}

type FuelTypesResponse struct {
	Code int      `json:"code"`
	Data []string `json:"data,omitempty"`
}

type BrowserFuelPrice struct {
	FuelType string  `json:"fuel_type"`
	Price    float64 `json:"price"`
}

type BrowserStation struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Lat      FlexFloat          `json:"lat"`
	Lng      FlexFloat          `json:"lng"`
	Type     string             `json:"type"`
	Prices   []BrowserFuelPrice `json:"prices,omitempty"`
	Address  string             `json:"address"`
	Phone    string             `json:"phone,omitempty"`
	PhoneURI string             `json:"phone_uri,omitempty"`
	Distance float64            `json:"distance"`
}

// validateQueryParameters checks that query parameters don't exceed reasonable size limits
func validateQueryParameters(r *http.Request) error {
	// Check total query string length
	rawQuery := r.URL.RawQuery
	if len(rawQuery) > MaxQueryStringLength {
		return fmt.Errorf("query string too long: %d chars (max %d)", len(rawQuery), MaxQueryStringLength)
	}

	// Check individual parameter values
	for key, values := range r.URL.Query() {
		for _, value := range values {
			if len(value) > MaxParameterValueLength {
				return fmt.Errorf("parameter %q value too long: %d chars (max %d)", key, len(value), MaxParameterValueLength)
			}
		}
	}

	return nil
}

// isValidLatitude checks if a float64 is a valid latitude value
func isValidLatitude(lat float64) bool {
	if math.IsNaN(lat) || math.IsInf(lat, 0) {
		return false
	}
	return lat >= MinLatitude && lat <= MaxLatitude
}

// isValidLongitude checks if a float64 is a valid longitude value
func isValidLongitude(lng float64) bool {
	if math.IsNaN(lng) || math.IsInf(lng, 0) {
		return false
	}
	return lng >= MinLongitude && lng <= MaxLongitude
}

// validateBboxRange checks that a bounding box has valid ranges
func validateBboxRange(minLat, minLng, maxLat, maxLng float64) error {
	// Check for NaN/Infinity
	if math.IsNaN(minLat) || math.IsInf(minLat, 0) || math.IsNaN(maxLat) || math.IsInf(maxLat, 0) ||
		math.IsNaN(minLng) || math.IsInf(minLng, 0) || math.IsNaN(maxLng) || math.IsInf(maxLng, 0) {
		return fmt.Errorf("bbox contains NaN or Infinity values")
	}

	// Check that min <= max (equal values allowed for single-point bbox)
	if minLat > maxLat {
		return fmt.Errorf("bbox minLat (%f) must be less than or equal to maxLat (%f)", minLat, maxLat)
	}
	if minLng > maxLng {
		return fmt.Errorf("bbox minLng (%f) must be less than or equal to maxLng (%f)", minLng, maxLng)
	}

	// Check that coordinates are within Earth bounds
	if minLat < MinLatitude || maxLat > MaxLatitude {
		return fmt.Errorf("bbox latitudes must be between %.1f and %.1f", MinLatitude, MaxLatitude)
	}
	if minLng < MinLongitude || maxLng > MaxLongitude {
		return fmt.Errorf("bbox longitudes must be between %.1f and %.1f", MinLongitude, MaxLongitude)
	}

	return nil
}

// http://0.0.0.0:8080/stations?fuel_type=E10&lat=53.483959&lng=-2.244644
func stationsAPIHandler(w http.ResponseWriter, r *http.Request) {
	// Validate query parameters size
	if err := validateQueryParameters(r); err != nil {
		log.Printf("Query validation error: %v", err)
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	fuelType := r.URL.Query().Get("fuel_type")
	if fuelType != "" && !fuelTypePattern.MatchString(fuelType) {
		http.Error(w, "Invalid fuel_type. Use 1-16 chars: A-Z, 0-9, and underscore only.", http.StatusBadRequest)
		return
	}

	if fuelType != "" {
		log.Printf("Filtering stations by fuel type: %s", fuelType)
	} else {
		log.Printf("No fuel type filter applied, returning all stations")
	}

	lat := r.URL.Query().Get("lat")
	lng := r.URL.Query().Get("lng")
	bboxProvided := false
	var minLat, minLng, maxLat, maxLng float64

	// only allow 0-9, dot and minus in lat/lng (only one decimal point allowed)
	latLngPattern := regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	if lat != "" && !latLngPattern.MatchString(lat) {
		http.Error(w, "Invalid lat parameter: must be a valid decimal number", http.StatusBadRequest)
		return
	}
	if lng != "" && !latLngPattern.MatchString(lng) {
		http.Error(w, "Invalid lng parameter: must be a valid decimal number", http.StatusBadRequest)
		return
	}

	// make a copy of the stations slice to avoid modifying the original in memorywhen sorting by distance
	stationsMutex.Lock()
	stationsToBeReturned := make([]Station, len(stations))
	copy(stationsToBeReturned, stations)
	stationsMutex.Unlock()

	log.Printf("Received request for stations with fuel type '%s' and location (%s, %s)", fuelType, lat, lng)

	fuelTypes := getCachedFuelTypes()
	// if fuelType matches any of the cached fuel types, log it
	if fuelType != "" {
		foundStationsWithFuelType := false
		for _, ft := range fuelTypes {
			if ft == fuelType {
				stationsToBeReturned = filterStationsByFuelType(stationsToBeReturned, fuelType)
				log.Printf("Found %d stations with fuel type %s", len(stationsToBeReturned), fuelType)
				foundStationsWithFuelType = true
				break
			}
		}
		if foundStationsWithFuelType {
			log.Printf("Requested fuel type %s is available in cached fuel types", fuelType)
		} else {
			log.Printf("Requested fuel type %s is NOT available in cached fuel types", fuelType)
		}
	}

	if lat != "" && lng != "" {
		log.Printf("Received location parameters: lat=%s, lng=%s", lat, lng)
	} else {
		log.Printf("No location parameters provided")
	}

	// If bounding box parameters provided, filter stations to those within the bounding box
	bbox := r.URL.Query().Get("bbox")
	if bbox != "" {
		bboxProvided = true
		log.Printf("Received bounding box parameter: %s", bbox)
		parts := strings.Split(bbox, ",")
		if len(parts) != 4 {
			http.Error(w, "Invalid bbox parameter. Use format: minLat,minLng,maxLat,maxLng", http.StatusBadRequest)
			return
		}
		var err1, err2, err3, err4 error
		minLat, err1 = strconv.ParseFloat(parts[0], 64)
		minLng, err2 = strconv.ParseFloat(parts[1], 64)
		maxLat, err3 = strconv.ParseFloat(parts[2], 64)
		maxLng, err4 = strconv.ParseFloat(parts[3], 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			http.Error(w, "Invalid bbox parameter. Use format: minLat,minLng,maxLat,maxLng with valid float values", http.StatusBadRequest)
			return
		}
		// Validate bbox ranges and values
		if err := validateBboxRange(minLat, minLng, maxLat, maxLng); err != nil {
			http.Error(w, "Invalid bbox: "+err.Error(), http.StatusBadRequest)
			return
		}
		stationsToBeReturned = filterStationsByBoundingBox(stationsToBeReturned, minLat, minLng, maxLat, maxLng)
		log.Printf("Filtered stations to %d within bounding box", len(stationsToBeReturned))
	}

	// If lat/lng provided, sort stations by distance to that location, otherwise return in order received from API/database
	if lat != "" && lng != "" {
		log.Printf("Sorting stations by distance to provided location (%s, %s)", lat, lng)
		// Convert lat/lng to float64
		latFloat, err1 := strconv.ParseFloat(lat, 64)
		lngFloat, err2 := strconv.ParseFloat(lng, 64)
		if err1 != nil || err2 != nil {
			http.Error(w, "Invalid lat or lng parameter", http.StatusBadRequest)
			return
		}
		// Validate coordinate ranges and check for NaN/Infinity
		if !isValidLatitude(latFloat) {
			http.Error(w, fmt.Sprintf("Invalid lat: must be between %.1f and %.1f", MinLatitude, MaxLatitude), http.StatusBadRequest)
			return
		}
		if !isValidLongitude(lngFloat) {
			http.Error(w, fmt.Sprintf("Invalid lng: must be between %.1f and %.1f", MinLongitude, MaxLongitude), http.StatusBadRequest)
			return
		}

		stationsToBeReturned = StationsByDistance(stationsToBeReturned, latFloat, lngFloat)

		log.Printf("Sorted stations by distance to location (%s, %s)", lat, lng)

		for i := range stationsToBeReturned {
			s := &stationsToBeReturned[i]
			// distance from me to station in km, using haversine formula
			distance := haversine(latFloat, lngFloat, float64(s.Location.Latitude), float64(s.Location.Longitude))
			// convert distance to miles, rounded to 2 decimal places. Pad with zeros if necessary to always show 2 decimal places
			s.Distance = math.Round(distance*0.621371*100) / 100
		}
		log.Printf("Calculated distance for each station from location (%s, %s)", lat, lng)

	} else {
		log.Printf("No location provided, returning stations in original order")
	}

	// If there are more than 100 stations to be returned, limit the response to 100 stations.
	if len(stationsToBeReturned) > 100 {
		if bboxProvided {
			log.Printf("More than 100 stations in bbox (%d), selecting 100 stations with spatial spread and cheaper-price preference", len(stationsToBeReturned))
			stationsToBeReturned = selectStationsForBoundingBox(stationsToBeReturned, 100, minLat, minLng, maxLat, maxLng)
		} else {
			log.Printf("More than 100 stations to be returned (%d), selecting the first 100 stations", len(stationsToBeReturned))
			stationsToBeReturned = selectFirstStations(stationsToBeReturned, 100)
		}
	}

	// Generate an API response with code 200 and the stations data.
	response := APIResponse{
		Code: 200,
		//Message: "success",
		Data: formattedStationsForJS(stationsToBeReturned),
	}

	if err := writeJSONPretty(w, response); err != nil {
		http.Error(w, "Failed to encode stations data", http.StatusInternalServerError)
		return
	}
}

func filterStationsByBoundingBox(stations []Station, minLat, minLng, maxLat, maxLng float64) []Station {
	filtered := make([]Station, 0, len(stations))
	for _, s := range stations {
		lat := float64(s.Location.Latitude)
		lng := float64(s.Location.Longitude)
		if lat >= minLat && lat <= maxLat && lng >= minLng && lng <= maxLng {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func selectFirstStations(stations []Station, n int) []Station {
	if len(stations) <= n {
		return stations
	}

	return stations[:n]
}

func buildLowestStationPriceIndex() map[string]float64 {
	lowestByNode := make(map[string]float64, len(priceStations))

	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	for _, station := range priceStations {
		lowest := math.MaxFloat64
		for _, fuelPrice := range station.FuelPrices {
			if fuelPrice.Price > 0 && fuelPrice.Price < lowest {
				lowest = fuelPrice.Price
			}
		}

		if lowest < math.MaxFloat64 {
			lowestByNode[station.NodeID] = lowest
		}
	}

	return lowestByNode
}

type stationCandidate struct {
	station Station
	price   float64
}

func selectStationsForBoundingBox(stations []Station, n int, minLat, minLng, maxLat, maxLng float64) []Station {
	if len(stations) <= n {
		return stations
	}

	const gridRows = 10
	const gridCols = 10

	latRange := maxLat - minLat
	if latRange <= 0 {
		latRange = 1
	}

	lngRange := maxLng - minLng
	if lngRange <= 0 {
		lngRange = 1
	}

	lowestByNode := buildLowestStationPriceIndex()
	buckets := make(map[int][]stationCandidate, gridRows*gridCols)

	for _, station := range stations {
		lat := float64(station.Location.Latitude)
		lng := float64(station.Location.Longitude)

		row := int(((lat - minLat) / latRange) * gridRows)
		col := int(((lng - minLng) / lngRange) * gridCols)

		if row < 0 {
			row = 0
		}
		if row >= gridRows {
			row = gridRows - 1
		}

		if col < 0 {
			col = 0
		}
		if col >= gridCols {
			col = gridCols - 1
		}

		price := math.MaxFloat64
		if p, ok := lowestByNode[station.NodeID]; ok {
			price = p
		}

		bucketID := row*gridCols + col
		buckets[bucketID] = append(buckets[bucketID], stationCandidate{station: station, price: price})
	}

	bucketIDs := make([]int, 0, len(buckets))
	for bucketID, candidates := range buckets {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].price == candidates[j].price {
				return candidates[i].station.NodeID < candidates[j].station.NodeID
			}
			return candidates[i].price < candidates[j].price
		})
		buckets[bucketID] = candidates
		bucketIDs = append(bucketIDs, bucketID)
	}

	sort.Ints(bucketIDs)

	selected := make([]Station, 0, n)
	nextIdxByBucket := make(map[int]int, len(bucketIDs))

	for len(selected) < n {
		pickedInRound := false
		for _, bucketID := range bucketIDs {
			nextIdx := nextIdxByBucket[bucketID]
			candidates := buckets[bucketID]
			if nextIdx >= len(candidates) {
				continue
			}

			selected = append(selected, candidates[nextIdx].station)
			nextIdxByBucket[bucketID] = nextIdx + 1
			pickedInRound = true

			if len(selected) >= n {
				break
			}
		}

		if !pickedInRound {
			break
		}
	}

	if len(selected) < n {
		fallback := selectFirstStations(stations, n)
		return fallback
	}

	return selected
}

func formattedStationsForJS(stations []Station) []BrowserStation {
	formatted := make([]BrowserStation, len(stations))
	for i, s := range stations {
		phone, phoneURI := sanitizePhoneForBrowser(s.PublicPhoneNumber)
		formatted[i] = BrowserStation{
			ID:       s.NodeID,
			Name:     sanitizeBrowserText(formatStationName(s), "Unnamed Station", 120),
			Lat:      s.Location.Latitude,
			Lng:      s.Location.Longitude,
			Type:     "landmark",
			Prices:   sanitizeFuelPricesForBrowser(getStationPrices(s)),
			Address:  sanitizeBrowserText(formatStationAddress(s), "No address available", 200),
			Phone:    phone,
			PhoneURI: phoneURI,
			Distance: sanitizeDistanceForBrowser(s.Distance),
		}
	}
	return formatted
}

func sanitizeBrowserText(value, fallback string, maxRunes int) string {
	normalized := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)

	normalized = strings.Join(strings.Fields(normalized), " ")
	normalized = strings.ReplaceAll(normalized, " ,", ",")
	if normalized == "" {
		normalized = fallback
	}

	if maxRunes > 0 {
		runes := []rune(normalized)
		if len(runes) > maxRunes {
			normalized = strings.TrimSpace(string(runes[:maxRunes]))
		}
	}

	if normalized == "" {
		return fallback
	}

	return normalized
}

func sanitizeFuelPricesForBrowser(prices []FuelPrice) []BrowserFuelPrice {
	sanitized := make([]BrowserFuelPrice, 0, len(prices))
	for _, price := range prices {
		fuelType := strings.TrimSpace(price.FuelType)
		if !fuelTypePattern.MatchString(fuelType) {
			continue
		}
		if math.IsNaN(price.Price) || math.IsInf(price.Price, 0) || price.Price < 0 || price.Price > 1000 {
			continue
		}

		sanitized = append(sanitized, BrowserFuelPrice{
			FuelType: fuelType,
			Price:    price.Price,
		})
	}
	return sanitized
}

func sanitizePhoneForBrowser(phone string) (string, string) {
	display := sanitizeBrowserText(phone, "", 32)
	if display == "" || !browserPhonePattern.MatchString(display) {
		return "", ""
	}

	uriDigits := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '+' {
			return r
		}
		return -1
	}, display)

	if !regexp.MustCompile(`^\+?[0-9]{3,20}$`).MatchString(uriDigits) {
		return "", ""
	}

	return display, "tel:" + uriDigits
}

func sanitizeDistanceForBrowser(distance float64) float64 {
	if math.IsNaN(distance) || math.IsInf(distance, 0) || distance < 0 {
		return 0
	}
	return distance
}

func formatStationName(s Station) string {
	if s.TradingName == "" && s.BrandName == "" {
		return "Unnamed Station"
	}

	if s.IsSameTradingAndBrandName {
		return s.BrandName
	}

	if s.TradingName == "" {
		return s.BrandName
	}

	if s.BrandName == "" {
		return s.TradingName
	}

	return fmt.Sprintf("%s - %s", s.BrandName, s.TradingName)
}

func getStationPrices(s Station) []FuelPrice {
	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	priceStation, exists := priceStationsIndex[s.NodeID]
	if !exists {
		return nil
	}

	// Return a copy so callers cannot observe or mutate the shared backing array.
	return append([]FuelPrice(nil), priceStations[priceStation].FuelPrices...)
}

func formatStationAddress(s Station) string {
	parts := []string{}
	for _, p := range []string{s.Location.AddressLine1, s.Location.AddressLine2, s.Location.City, s.Location.County, s.Location.Postcode} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func fuelTypesAPIHandler(w http.ResponseWriter, r *http.Request) {
	fuelTypes := getCachedFuelTypes()

	response := FuelTypesResponse{
		Code: 200,
		Data: fuelTypes,
	}

	if err := writeJSONPretty(w, response); err != nil {
		http.Error(w, "Failed to encode fuel types data", http.StatusInternalServerError)
		return
	}
}
