// a router or web handler or server that can return database data in a human readable format online
package main

import (
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var fuelTypePattern = regexp.MustCompile(`^[A-Z0-9_]{1,16}$`)

// a map response will have a key of code (int), a key of message (string) and a key of data (interface{})
type APIResponse struct {
	Code int `json:"code"`
	//Message string      `json:"message"`
	Data interface{} `json:"data,omitempty"`
}

type FuelTypesResponse struct {
	Code int      `json:"code"`
	Data []string `json:"data,omitempty"`
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
	bboxProvided := false
	var minLat, minLng, maxLat, maxLng float64

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

	// if there are more than 500 stations to be returned, select 500 random stations
	if len(stationsToBeReturned) > 500 {
		if bboxProvided {
			log.Printf("More than 500 stations in bbox (%d), selecting 500 stations with spatial spread and cheaper-price preference", len(stationsToBeReturned))
			stationsToBeReturned = selectStationsForBoundingBox(stationsToBeReturned, 500, minLat, minLng, maxLat, maxLng)
		} else {
			log.Printf("More than 500 stations to be returned (%d), selecting 500 random stations", len(stationsToBeReturned))
			stationsToBeReturned = selectFirstStations(stationsToBeReturned, 500)
		}
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

	selected := make([]Station, n)
	perm := rand.Perm(len(stations))
	for i := 0; i < n; i++ {
		selected[i] = stations[perm[i]]
	}

	return selected
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

func formattedStationsForJS(stations []Station) []map[string]interface{} {
	// lock the stations mutex while we read from the stations slice to avoid concurrent modification issues
	formatted := make([]map[string]interface{}, len(stations))
	for i, s := range stations {
		formatted[i] = map[string]interface{}{
			"id":       s.NodeID,
			"name":     formatStationName(s),
			"lat":      s.Location.Latitude,
			"lng":      s.Location.Longitude,
			"type":     "landmark",
			"prices":   getStationPrices(s),
			"address":  formatStationAddress(s),
			"phone":    s.PublicPhoneNumber,
			"distance": s.Distance,
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
