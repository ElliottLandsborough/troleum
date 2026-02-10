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

// Up to 10,000 stations expected (~8000 in the uk)
var stations = make([]Station, 0, 10000)
var stationsIndex = make(map[string]int, 10000)

// Up to 10,000 price entries expected, same count as stations
var priceStations = make([]PriceStation, 0, 10000)
var priceStationsIndex = make(map[string]int, 10000)

// Map of indexed json files already saved with their page numbers and datestamps
var savedStationsPages = make(map[int]string)
var savedPricesPages = make(map[int]string)

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

// List the current json files, if they exist, into the savedStationsPages and savedPricesPages maps
func initializeSavedPages() {
	priceRequests, err := GetMostRecentSuccessfulRequests(RequestTypePricesPage, 100) // Populate savedPricesPages
	if err != nil {
		log.Printf("Error getting most recent successful price requests: %v", err)
	}

	// loop through the results and populate savedPricesPages with page number and created_at timestamp
	for _, req := range priceRequests {
		pageNum := req.PageNumber
		createdAt := req.CreatedAt
		savedPricesPages[pageNum] = createdAt.String()
		json := req.Data
		log.Printf("Page %d - Created At: %s - Data Preview: %s", pageNum, createdAt.String(), json)

		// unmarshal the prices request data from json into the globally available priceStations slice and index
		//var response PriceStationResponse
		//if err := json.Unmarshal([]byte(json), &response); err != nil {
		//	log.Printf("Error unmarshalling price request data for page %d: %v", pageNum, err)
		//	continue
		//}
		//mergePriceStations(response.Data)
	}

	stationRequests, err := GetMostRecentSuccessfulRequests(RequestTypeStationsPage, 100) // Populate savedStationsPages
	if err != nil {
		log.Printf("Error getting most recent successful station requests: %v", err)
	}

	// loop through the results and populate savedStationsPages with page number and created_at timestamp
	for _, req := range stationRequests {
		pageNum := req.PageNumber
		createdAt := req.CreatedAt
		savedStationsPages[pageNum] = createdAt.String()
		json := req.Data
		log.Printf("Page %d - Created At: %s - Data Preview: %s", pageNum, createdAt.String(), json)
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

// Merges newStations into the global stations slice, avoiding duplicates based on NodeID
func mergeStations(newStations []Station) {
	for _, newStation := range newStations {
		if _, exists := stationsIndex[newStation.NodeID]; !exists {
			stationsIndex[newStation.NodeID] = len(stations)
			stations = append(stations, newStation)
		}
	}

	mergeStationLocations(newStations)
}

// Merges newPriceStations into the global priceStations slice, avoiding duplicates based on NodeID
func mergePriceStations(newPriceStations []PriceStation) {
	for _, newPriceStation := range newPriceStations {
		if _, exists := priceStationsIndex[newPriceStation.NodeID]; !exists {
			priceStationsIndex[newPriceStation.NodeID] = len(priceStations)
			priceStations = append(priceStations, newPriceStation)
		}
	}
}

type LatLon struct {
	Lat float64
	Lon float64
}

// a map with the key node_id and the value LatLon
var stationLocations = make(map[string]LatLon)

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

// Merges station locations from newStations into the stationLocations map
func mergeStationLocations(newStations []Station) {
	for _, newStation := range newStations {
		if _, exists := stationLocations[newStation.NodeID]; !exists {
			latitudeString := strings.TrimSpace(newStation.Location.Latitude)
			longitudeString := strings.TrimSpace(newStation.Location.Longitude)
			if latitudeString == "" || longitudeString == "" {
				continue
			}
			latitude, _ := strconv.ParseFloat(latitudeString, 64)
			longitude, _ := strconv.ParseFloat(longitudeString, 64)
			if latitude == 0 || longitude == 0 {
				continue
			}
			stationLocations[newStation.NodeID] = LatLon{
				Lat: latitude,
				Lon: longitude,
			}
		}
	}
}

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

// GetLatestSuccessfulRequests returns the latest successful (status 200) request for each unique page number by request type
func GetLatestSuccessfulRequests(requestType RequestType) ([]RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

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

// GetMostRecentSuccessfulRequests returns the most recent successful (status 200) requests, ordered by timestamp
func GetMostRecentSuccessfulRequests(requestType RequestType, limit int) ([]RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

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

// GetHighestSuccessfulPageNumber returns the highest page number that has a successful request
func GetHighestSuccessfulPageNumber(requestType RequestType) (int, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

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

// GetMostRecentSuccessfulPage returns the most recent successful request for the highest available page
func GetMostRecentSuccessfulPage(requestType RequestType) (*RequestLog, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// First get the highest page number
	maxPage, err := GetHighestSuccessfulPageNumber(requestType)
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
