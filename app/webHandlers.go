// a router or web handler or server that can return database data in a human readable format online
package main

import (
	"net/http"
	"strconv"
)

func stationsAPIHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	/*
		perPageStr := r.URL.Query().Get("per_page")
		perPage := 20 // default
		if perPageStr != "" {
			if p, err := strconv.Atoi(perPageStr); err == nil && p > 0 {
				perPage = p
			}
		}
	*/

	location := r.URL.Query().Get("location")
	fuelType := r.URL.Query().Get("fuel_type")

	// Fetch stations from database with optional filtering/sorting
	_, err := GetStationsFromDatabase(location, fuelType)
	if err != nil {
		http.Error(w, "Failed to get stations: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stationLocationsMutex.Lock()
	val := stationLocations
	stationLocationsMutex.Unlock()

	// Return stations as JSON
	if err := writeJSONPretty(w, val); err != nil {
		http.Error(w, "Failed to encode stations data", http.StatusInternalServerError)
	}
}

// todo: move to database.go
func GetStationsFromDatabase(location string, fuelType string) ([]Station, error) {
	// This function should query the database for stations, applying any necessary filters or sorting based on the parameters.
	// For example, it might use SQL queries to filter by fuel type or sort by distance if location is provided.
	// The implementation details would depend on the database schema and the ORM or database library being used.

	// Placeholder implementation - replace with actual database query logic
	return []Station{}, nil

	/*
			        {
		                id: 1,
		                name: "Eiffel Tower",
		                lat: 48.8584,
		                lng: 2.2945,
		                city: "Paris, France",
		                description: "Iconic iron lattice tower on the Champ de Mars",
		                type: "landmark"
		            },
	*/
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
