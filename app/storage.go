package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type LatLon struct {
	Lat float64
	Lon float64
}

// a map with the key node_id and the value LatLon
var stationLocations = make(map[string]LatLon)

// Up to 10,000 stations expected (~8000 in the uk)
var stations = make([]Station, 0, 10000)
var stationsIndex = make(map[string]int, 10000)

// Up to 10,000 price entries expected, same count as stations
var priceStations = make([]PriceStation, 0, 10000)
var priceStationsIndex = make(map[string]int, 10000)

// Map of indexed json files already saved with their page numbers and datestamps
var savedStationsPages = make(map[int]string)
var savedPricesPages = make(map[int]string)

// Mutex for thread-safe access to savedStationsPages and savedPricesPages maps
var savedPagesMutex sync.Mutex

// Mutex for thread-safe access to stations slice and index
var stationsMutex sync.Mutex

// Mutex for thread-safe access to priceStations slice and index
var priceStationsMutex sync.Mutex

// update the savedStationsPages or savedPricesPages map with the last modified time of the saved file
func storeSavedPage(pageMap map[int]string, mutex *sync.Mutex, pageNum int, filePath string) {
	mutex.Lock()
	defer mutex.Unlock()

	info, err := os.Stat(filePath)
	if err == nil {
		modTime := info.ModTime()
		pageMap[pageNum] = modTime.String()
	}
}

// NodeIDEntity interface for entities that have a NodeID field
type NodeIDEntity interface {
	GetNodeID() string
}

// Implement NodeIDEntity interface for Station
func (s Station) GetNodeID() string {
	return s.NodeID
}

// Implement NodeIDEntity interface for PriceStation
func (p PriceStation) GetNodeID() string {
	return p.NodeID
}

// Generic JSON processing function that handles both wrapped and direct array formats
func processJSONArray[T any](jsonData string, pageNum int, dataType string) ([]T, error) {
	if jsonData == "" {
		return nil, fmt.Errorf("no data found for page %d", pageNum)
	}

	rawMessage := json.RawMessage(jsonData)
	var result []T

	// First try to unmarshal as wrapped response (with "success" and "data" fields)
	var wrappedResponse map[string]interface{}
	err := json.Unmarshal(rawMessage, &wrappedResponse)
	if err == nil {
		if dataArray, ok := wrappedResponse["data"]; ok {
			// Re-marshal the data array and unmarshal into our result
			dataJSON, err := json.Marshal(dataArray)
			if err == nil {
				err = json.Unmarshal(dataJSON, &result)
				if err == nil {
					return result, nil
				}
			}
		}
	}

	// If wrapped response fails, try to unmarshal as direct array
	err = json.Unmarshal(rawMessage, &result)
	if err != nil {
		dataLen := len(jsonData)
		preview := jsonData
		// todo: why is this 200 here and there are mixed 200/100 elsewhere? are they linked?
		if dataLen > 200 {
			preview = jsonData[:200] + "..."
		}
		return nil, fmt.Errorf("error unmarshalling %s data for page %d: %v (data length: %d, preview: %s)",
			dataType, pageNum, err, dataLen, preview)
	}

	return result, nil
}

// Generic merge function for entities with NodeID
func mergeEntities[T NodeIDEntity](newEntities []T, globalSlice *[]T, globalIndex map[string]int, mutex *sync.Mutex) {
	mutex.Lock()
	defer mutex.Unlock()

	for _, entity := range newEntities {
		nodeID := entity.GetNodeID()
		if _, exists := globalIndex[nodeID]; !exists {
			globalIndex[nodeID] = len(*globalSlice)
			*globalSlice = append(*globalSlice, entity)
		}
	}
}

// List the current json files, if they exist, into the savedStationsPages and savedPricesPages maps
func enrichSavedPages() {
	// Process price data
	priceResponses, err := GetFullDataForEnrichment(RequestTypePricesPage, 100)
	if err != nil {
		log.Printf("[ENRICH] Error getting full price data for enrichment: %v", err)
	}

	for _, req := range priceResponses {
		pageNum := req.PageNumber
		createdAt := req.CreatedAt
		savedPricesPages[pageNum] = createdAt.String()

		priceStationsList, err := processJSONArray[PriceStation](req.Data, pageNum, "price")
		if err != nil {
			log.Printf("[ENRICH] %v", err)
			continue
		}

		mergeEntities(priceStationsList, &priceStations, priceStationsIndex, &priceStationsMutex)
	}

	// Process station data
	stationRequests, err := GetFullDataForEnrichment(RequestTypeStationsPage, 100)
	if err != nil {
		log.Printf("[ENRICH] Error getting full station data for enrichment: %v", err)
	}

	for _, req := range stationRequests {
		pageNum := req.PageNumber
		createdAt := req.CreatedAt
		savedStationsPages[pageNum] = createdAt.String()

		stationList, err := processJSONArray[Station](req.Data, pageNum, "station")
		if err != nil {
			log.Printf("[ENRICH] %v", err)
			continue
		}

		mergeEntities(stationList, &stations, stationsIndex, &stationsMutex)

		// an 'array' of stations with their lat lon locations only
		mergeStationLocations(stationList)
	}
}

func clearSavedPages() {
	savedPagesMutex.Lock()
	defer savedPagesMutex.Unlock()
	savedStationsPages = make(map[int]string)
	savedPricesPages = make(map[int]string)
}

func getPricesPageFilePath(pageNum int) string {
	return "json/prices_page_" + strconv.Itoa(pageNum) + ".json"
}

func getStationsPageFilePath(pageNum int) string {
	return "json/stations_page_" + strconv.Itoa(pageNum) + ".json"
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// StationsResponse represents the API response structure for stations
type StationsResponse struct {
	Success bool      `json:"success"`
	Data    []Station `json:"data"`
}

// readStationsFromFile reads a JSON file and unmarshals it into a slice of stations
func readStationsFromFile(filePath string) ([]Station, error) {
	// Read the file contents
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Unmarshal the JSON into StationsResponse structure
	var response StationsResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, err
	}

	// Return the stations data
	return response.Data, nil
}

// PriceStationResponse represents the API response structure for price stations
type PriceStationResponse struct {
	Success bool           `json:"success"`
	Data    []PriceStation `json:"data"`
}

// readPricesFromFile reads a JSON file and unmarshals it into a slice of price stations
func readPricesStationsFromFile(filePath string) ([]PriceStation, error) {
	// Read the file contents
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Unmarshal the JSON into PriceResponse structure
	var response PriceStationResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, err
	}

	// Return the prices data
	return response.Data, nil
}

// Database connection
var db *sql.DB
var dbMutex sync.Mutex

// RequestType represents the type of API request
type RequestType string

const (
	RequestTypeStationsPage RequestType = "stations_page"
	RequestTypePricesPage   RequestType = "prices_page"
)

// RequestLog represents a database record for API requests
type RequestLog struct {
	ID           string      `db:"id"`
	RequestType  RequestType `db:"request_type"` // "stations_page" or "prices_page"
	PageNumber   int         `db:"page_number"`
	StatusCode   int         `db:"status_code"`
	Data         string      `db:"data"`
	CreatedAt    time.Time   `db:"created_at"`
	ErrorMessage string      `db:"error_message"`
}

// ProcessJSONFromAnywhere - public function to process JSON data from anywhere in the app
func ProcessJSONFromAnywhere(jsonData string, dataType string) error {
	switch strings.ToLower(dataType) {
	case "stations", "station":
		stationList, err := processJSONArray[Station](jsonData, 0, "station")
		if err != nil {
			return err
		}
		mergeEntities(stationList, &stations, stationsIndex, &stationsMutex)
		mergeStationLocations(stationList)
		return nil

	case "prices", "price":
		priceStationsList, err := processJSONArray[PriceStation](jsonData, 0, "price")
		if err != nil {
			return err
		}
		mergeEntities(priceStationsList, &priceStations, priceStationsIndex, &priceStationsMutex)
		//mergeFuelPrices(priceStationsList)
		return nil

	default:
		return fmt.Errorf("unknown data type: %s (supported: stations, prices)", dataType)
	}
}

// Merges station locations from newStations into the stationLocations map
func mergeStationLocations(newStations []Station) {
	for _, newStation := range newStations {
		if _, exists := stationLocations[newStation.NodeID]; !exists {
			stationLocations[newStation.NodeID] = LatLon{
				Lat: float64(newStation.Location.Latitude),
				Lon: float64(newStation.Location.Longitude),
			}
		}
	}
}

/*
// Merges fuel prices from newPriceStations into the priceStations slice and index
func mergeFuelPrices(newPriceStations []PriceStation) {
	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	for _, newPriceStation := range newPriceStations {
		nodeID := newPriceStation.NodeID
		if _, exists := priceStationsIndex[nodeID]; !exists {
			priceStationsIndex[nodeID] = len(priceStations)
			priceStations = append(priceStations, newPriceStation)
		} else {
			// If the station already exists, we can choose to update the price information if needed
			existingIndex := priceStationsIndex[nodeID]
			priceStations[existingIndex].DieselPrice = newPriceStation.DieselPrice
			priceStations[existingIndex].Petrol95Price = newPriceStation.Petrol95Price
			priceStations[existingIndex].Petrol98Price = newPriceStation.Petrol98Price
		}
	}
}
*/

// InitDatabase initializes the database connection and creates necessary tables
func InitDatabase() error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	// Connect to PostgreSQL database
	dbHost := getEnvWithDefault("DB_HOST", "localhost")
	dbPort := getEnvWithDefault("DB_PORT", "5432")
	dbUser := getEnvWithDefault("DB_USER", "postgres")
	dbPassword := getEnvWithDefault("DB_PASSWORD", "password")
	dbName := getEnvWithDefault("DB_NAME", "postgres")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %v", err)
	}

	// Create the request_logs table if it doesn't exist
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS request_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			request_type VARCHAR(15) NOT NULL, -- 'stations_page' or 'prices_page'
			page_number INTEGER NOT NULL,
			status_code INTEGER NOT NULL,
			data TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			error_message TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_request_logs_type_page ON request_logs(request_type, page_number);
		CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status_code);
	`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create request_logs table: %v", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

// SaveRequestToDatabase saves a request log entry to the database
func SaveRequestToDatabase(requestType RequestType, pageNumber int, statusCode int, data string, errorMessage string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	query := `
		INSERT INTO request_logs (request_type, page_number, status_code, data, error_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := db.Exec(query, string(requestType), pageNumber, statusCode, data, errorMessage, time.Now())
	if err != nil {
		return fmt.Errorf("failed to insert request log: %v", err)
	}

	return nil
}

// GetLatestSuccessfulRequestsFromDatabase returns the latest successful (status 200) request for each unique page number by request type
// Example: If you've fetched pages 1-16, returns 16 results (one per page)
func GetLatestSuccessfulRequestsFromDatabase(requestType RequestType) ([]RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// todo: when parsing, why limit this val to 100 with a substring?
	query := `
		SELECT DISTINCT ON (page_number) 
			id, request_type, page_number, status_code, 
			substring(data from 1 for 100) as data_preview, 
			created_at, error_message
		FROM request_logs 
		WHERE request_type = $1 AND status_code = 200
		ORDER BY page_number, created_at DESC
	`

	rows, err := db.Query(query, string(requestType))
	if err != nil {
		return nil, fmt.Errorf("failed to query latest successful requests: %v", err)
	}
	defer rows.Close()

	var results []RequestLog
	for rows.Next() {
		var log RequestLog
		var dataPreview string
		err := rows.Scan(&log.ID, &log.RequestType, &log.PageNumber,
			&log.StatusCode, &dataPreview, &log.CreatedAt, &log.ErrorMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request log: %v", err)
		}
		log.Data = dataPreview // Only preview for performance
		results = append(results, log)
	}

	return results, nil
}

// GetRequestStats returns statistics about requests in the database
func GetRequestStats() (map[string]interface{}, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	statsQuery := `
		SELECT 
			request_type,
			status_code,
			COUNT(*) as count
		FROM request_logs 
		GROUP BY request_type, status_code
		ORDER BY request_type, status_code
	`

	rows, err := db.Query(statsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query request stats: %v", err)
	}
	defer rows.Close()

	stats := make(map[string]interface{})
	for rows.Next() {
		var requestType string
		var statusCode, count int
		err := rows.Scan(&requestType, &statusCode, &count)
		if err != nil {
			return nil, fmt.Errorf("failed to scan stats: %v", err)
		}

		key := fmt.Sprintf("%s_%d", requestType, statusCode)
		stats[key] = count
	}

	return stats, nil
}

// GetFullDataForEnrichment returns complete data (not truncated) for enrichment processing
func GetFullDataForEnrichment(requestType RequestType, limit int) ([]RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT 
			id, request_type, page_number, status_code, 
			data, created_at, error_message
		FROM (
			SELECT 
				id, request_type, page_number, status_code, 
				data, created_at, error_message,
				ROW_NUMBER() OVER (PARTITION BY page_number ORDER BY created_at DESC) as rn
			FROM request_logs 
			WHERE request_type = $1 AND status_code = 200
		) ranked
		WHERE rn = 1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := db.Query(query, string(requestType), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query full data for enrichment: %v", err)
	}
	defer rows.Close()

	var results []RequestLog
	for rows.Next() {
		var log RequestLog
		err := rows.Scan(&log.ID, &log.RequestType, &log.PageNumber,
			&log.StatusCode, &log.Data, &log.CreatedAt, &log.ErrorMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request log: %v", err)
		}
		results = append(results, log)
	}

	return results, nil
}

// GetMostRecentSuccessfulRequests returns the most recent successful (status 200) requests, ordered by timestamp
// Example: If you've fetched pages 1-16 multiple times, returns only the 10 most recent (with limit=10)
func GetMostRecentSuccessfulRequestsFromDatabase(requestType RequestType, limit int) ([]RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// todo: when parsing, why limit this val to 200 with a substring?
	query := `
		SELECT 
			id, request_type, page_number, status_code, 
			substring(data from 1 for 200) as data_preview, 
			created_at, error_message
		FROM (
			SELECT 
				id, request_type, page_number, status_code, 
				data, created_at, error_message,
				ROW_NUMBER() OVER (PARTITION BY page_number ORDER BY created_at DESC) as rn
			FROM request_logs 
			WHERE request_type = $1 AND status_code = 200
		) ranked
		WHERE rn = 1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := db.Query(query, string(requestType), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query most recent successful requests: %v", err)
	}
	defer rows.Close()

	var results []RequestLog
	for rows.Next() {
		var log RequestLog
		var dataPreview string
		err := rows.Scan(&log.ID, &log.RequestType, &log.PageNumber,
			&log.StatusCode, &dataPreview, &log.CreatedAt, &log.ErrorMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request log: %v", err)
		}
		log.Data = dataPreview
		results = append(results, log)
	}

	return results, nil
}

// getHighestSuccessfulPageNumber returns the highest page number (assumes mutex is already locked)
func getHighestSuccessfulPageNumber(requestType RequestType) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT COALESCE(MAX(page_number), 0) 
		FROM request_logs 
		WHERE request_type = $1 AND status_code = 200
	`

	var maxPage int
	err := db.QueryRow(query, string(requestType)).Scan(&maxPage)
	if err != nil {
		return 0, fmt.Errorf("failed to query highest successful page: %v", err)
	}

	return maxPage, nil
}

// GetHighestSuccessfulPageNumber returns the highest page number that has a successful request
func GetHighestSuccessfulPageNumber(requestType RequestType) (int, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()
	return getHighestSuccessfulPageNumber(requestType)
}

// GetMostRecentSuccessfulPageFromDatabase returns the most recent successful request for the highest available page
func GetMostRecentSuccessfulPageFromDatabase(requestType RequestType) (*RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// First get the highest page number (using internal helper to avoid deadlock)
	maxPage, err := getHighestSuccessfulPageNumber(requestType)
	if err != nil {
		return nil, err
	}

	if maxPage == 0 {
		return nil, fmt.Errorf("no successful requests found for request type: %s", requestType)
	}

	// Then get the most recent successful request for that page
	query := `
		SELECT 
			id, request_type, page_number, status_code, 
			data, created_at, error_message
		FROM request_logs 
		WHERE request_type = $1 AND page_number = $2 AND status_code = 200
		ORDER BY created_at DESC
		LIMIT 1
	`

	var log RequestLog
	err = db.QueryRow(query, string(requestType), maxPage).Scan(
		&log.ID, &log.RequestType, &log.PageNumber,
		&log.StatusCode, &log.Data, &log.CreatedAt, &log.ErrorMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to query most recent successful page: %v", err)
	}

	return &log, nil
}

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
