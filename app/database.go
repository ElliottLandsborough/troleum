package main

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// Database connection
var db *sql.DB
var dbMutex sync.Mutex

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
