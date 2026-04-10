// a router or web handler or server that can return database data in a human readable format online
package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var fuelTypePattern = regexp.MustCompile(`^[A-Z0-9_]{1,16}$`)

// a map response will have a key of code (int), a key of message (string) and a key of data (interface{})
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// http://0.0.0.0:8080/stations?fuel_type=E10&lat=53.483959&lng=-2.244644
func stationsAPIHandler(w http.ResponseWriter, r *http.Request) {
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

	// only allow 0-9, dot and minus in lat/lng
	latLngPattern := regexp.MustCompile(`^-?[0-9.]+$`)
	if lat != "" && !latLngPattern.MatchString(lat) {
		http.Error(w, "Invalid lat parameter", http.StatusBadRequest)
		return
	}
	if lng != "" && !latLngPattern.MatchString(lng) {
		http.Error(w, "Invalid lng parameter", http.StatusBadRequest)
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
				stationsToBeReturned = filterStationsByFuelType(stations, fuelType)
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

		stationsToBeReturned = StationsByDistance(stationsToBeReturned, latFloat, lngFloat)

		log.Printf("Sorted stations by distance to location (%s, %s)", lat, lng)
	} else {
		log.Printf("No location provided, returning stations in original order")
	}

	// if there are more than 1000 stations to be returned, select 1000 random stations to return and log that we are doing this
	if len(stationsToBeReturned) > 1000 {
		log.Printf("More than 1000 stations to be returned (%d), selecting 1000 random stations to return", len(stationsToBeReturned))
		stationsToBeReturned = selectRandomStations(stationsToBeReturned, 1000)
	}

	// generate an API response with code 200, message "Success", and the stations data, and write it as pretty JSON to the response
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

func selectRandomStations(stations []Station, n int) []Station {
	if len(stations) <= n {
		return stations
	}

	selected := make([]Station, n)
	perm := rand.Perm(len(stations))
	for i := 0; i < n; i++ {
		selected[i] = stations[perm[i]]
	}
	return selected
}

func getFuelTypes(fuelPrices []FuelPrice) []string {
	fuelTypes := make([]string, len(fuelPrices))
	for i, price := range fuelPrices {
		fuelTypes[i] = price.FuelType
	}
	return fuelTypes
}

func formattedStationsForJS(stations []Station) []map[string]interface{} {
	// lock the stations mutex while we read from the stations slice to avoid concurrent modification issues
	formatted := make([]map[string]interface{}, len(stations))
	for i, s := range stations {
		formatted[i] = map[string]interface{}{
			"id":          s.NodeID,
			"name":        formatStationName(s),
			"lat":         s.Location.Latitude,
			"lng":         s.Location.Longitude,
			"city":        s.Location.City,
			"description": formatStationDescription(s),
			"type":        "landmark",
		}
	}
	return formatted
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

func formatStationDescription(s Station) string {
	var parts []string

	// Add telephone if not blank
	if s.PublicPhoneNumber != "" {
		parts = append(parts, fmt.Sprintf("📞 <a href=\"tel:%s\">%s</a>", s.PublicPhoneNumber, s.PublicPhoneNumber))
	}

	// Add address if not blank
	address := formatStationAddress(s)
	if address != "" {
		parts = append(parts, fmt.Sprintf("📍 %s", address))
	}

	return strings.Join(parts, "<br />\n")
}

func formatStationAddress(s Station) string {
	parts := []string{}
	for _, p := range []string{s.Location.AddressLine1, s.Location.AddressLine2, s.Location.City, s.Location.Country, s.Location.County, s.Location.Postcode} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// Handler to return latest successful stations requests from database
func savedStationsHandler(w http.ResponseWriter, r *http.Request) {
	stations, err := GetLatestSuccessfulRequestsFromDatabase(RequestTypeStationsPage)
	if err != nil {
		http.Error(w, "Failed to get stations from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, stations); err != nil {
		http.Error(w, "Failed to encode stations data", http.StatusInternalServerError)
	}
}

// Handler to return latest successful prices requests from database
func savedPricesHandler(w http.ResponseWriter, r *http.Request) {
	prices, err := GetLatestSuccessfulRequestsFromDatabase(RequestTypePricesPage)
	if err != nil {
		http.Error(w, "Failed to get prices from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, prices); err != nil {
		http.Error(w, "Failed to encode prices data", http.StatusInternalServerError)
	}
}

// Handler to return database statistics
func dbStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := GetRequestStats()
	if err != nil {
		http.Error(w, "Failed to get database stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, stats); err != nil {
		http.Error(w, "Failed to encode stats", http.StatusInternalServerError)
	}
}

// Handler to return most recent successful stations requests
func recentStationsHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	stations, err := GetMostRecentSuccessfulRequestsFromDatabase(RequestTypeStationsPage, limit)
	if err != nil {
		http.Error(w, "Failed to get recent stations: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, stations); err != nil {
		http.Error(w, "Failed to encode recent stations", http.StatusInternalServerError)
	}
}

// Handler to return most recent successful prices requests
func recentPricesHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	prices, err := GetMostRecentSuccessfulRequestsFromDatabase(RequestTypePricesPage, limit)
	if err != nil {
		http.Error(w, "Failed to get recent prices: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, prices); err != nil {
		http.Error(w, "Failed to encode recent prices", http.StatusInternalServerError)
	}
}

// Handler to return the most recent successful page for stations
func latestStationPageHandler(w http.ResponseWriter, r *http.Request) {
	latestPage, err := GetMostRecentSuccessfulPageFromDatabase(RequestTypeStationsPage)
	if err != nil {
		http.Error(w, "Failed to get latest station page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, latestPage); err != nil {
		http.Error(w, "Failed to encode latest station page", http.StatusInternalServerError)
	}
}

// Handler to return the most recent successful page for prices
func latestPricePageHandler(w http.ResponseWriter, r *http.Request) {
	latestPage, err := GetMostRecentSuccessfulPageFromDatabase(RequestTypePricesPage)
	if err != nil {
		http.Error(w, "Failed to get latest price page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONPretty(w, latestPage); err != nil {
		http.Error(w, "Failed to encode latest price page", http.StatusInternalServerError)
	}
}
