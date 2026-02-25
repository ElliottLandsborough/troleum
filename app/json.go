package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StationsResponse represents the API response structure for stations
type StationsResponse struct {
	Success bool      `json:"success"`
	Data    []Station `json:"data"`
}

// PriceStationResponse represents the API response structure for price stations
type PriceStationResponse struct {
	Success bool           `json:"success"`
	Data    []PriceStation `json:"data"`
}

// writeJSONPretty writes data as pretty-printed JSON when in debug mode
func writeJSONPretty(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")

	if debug {
		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(jsonData)
		return err
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(jsonData)
	return err
}

// FlexFloat is a custom type that can unmarshal from both string and float64
type FlexFloat float64

// UnmarshalJSON implements custom JSON unmarshaling for FlexFloat
func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a number first
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexFloat(num)
		return nil
	}

	// If that fails, try to unmarshal as a string and convert
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	// Convert string to float64
	num, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return fmt.Errorf("cannot convert %q to float64: %w", str, err)
	}

	*f = FlexFloat(num)
	return nil
}

// shouldCreateNewJSONFile checks if we need to create a new JSON file
// based on the age of existing timestamp-named files
func shouldCreateNewJSONFile() bool {
	dir := "json"

	// Check if directory exists and read entries
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist or can't read it, so create new file
		return true
	}

	var latestTimestamp int64 = 0

	// Find the most recent timestamp
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Parse filename as Unix timestamp
		var timestamp int64
		if _, err := fmt.Sscanf(entry.Name(), "%d", &timestamp); err == nil {
			if timestamp > latestTimestamp {
				latestTimestamp = timestamp
			}
		}
	}

	// If no valid timestamp files found, create new file
	if latestTimestamp == 0 {
		return true
	}

	// Check if latest file is older than 1 hour
	latestTime := time.Unix(latestTimestamp, 0)
	return time.Since(latestTime) > time.Hour
}

func savePageJSON(jsonString string, pageNumber int, logName string) (string, error) {
	dir := "json"
	filename := fmt.Sprintf("%s_page_%d.json", logName, pageNumber)
	fullPath := filepath.Join(dir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	f, err := os.OpenFile(
		fullPath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0600,
	)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(jsonString)
	return fullPath, nil
}

// Get contents of latest json file
func getLatestJSONFileContents() (string, error) {
	dir := "json"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var latestFile string
	var latestTimestamp int64 = 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var timestamp int64
		if _, err := fmt.Sscanf(entry.Name(), "%d", &timestamp); err == nil {
			if timestamp > latestTimestamp {
				latestTimestamp = timestamp
				latestFile = entry.Name()
			}
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no JSON files found")
	}

	fullPath := filepath.Join(dir, latestFile)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func isJSONFileRecentEnough(filePath string, maxAgeMinutes int) bool {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		// File doesn't exist or can't be accessed
		return false
	}

	maxAge := time.Duration(maxAgeMinutes) * time.Minute
	return time.Since(fileInfo.ModTime()) < maxAge
}

// Generic JSON processing function that handles both wrapped and direct array formats
func processJSONArray[T any](jsonData json.RawMessage, pageNum int, dataType RequestType) ([]T, error) {
	if len(jsonData) == 0 {
		return nil, fmt.Errorf("no data found for page %d", pageNum)
	}

	var result []T

	// First try to unmarshal as wrapped response (with "success" and "data" fields)
	var wrappedResponse map[string]any
	err := json.Unmarshal(jsonData, &wrappedResponse)
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
	err = json.Unmarshal(jsonData, &result)
	if err != nil {
		preview := string(jsonData)
		dataLen := len(preview)
		if dataLen > JSONPreviewLength {
			preview = preview[:JSONPreviewLength] + "..."
		}
		return nil, fmt.Errorf("error unmarshalling %s data for page %d: %v (data length: %d, preview: %s)",
			dataType, pageNum, err, dataLen, preview)
	}

	return result, nil
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

// loadDataFromJSONFiles loads data from existing JSON files into memory on startup
func loadDataFromJSONFiles() {
	entries, err := os.ReadDir("json")
	if err != nil {
		log.Printf("[STARTUP] Error reading json directory: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, "stations_page_") && strings.HasSuffix(name, ".json") {
			pageNumStr := strings.TrimSuffix(strings.TrimPrefix(name, "stations_page_"), ".json")
			pageNum, err := strconv.Atoi(pageNumStr)
			if err != nil {
				log.Printf("[STARTUP] Error parsing page number from filename %s: %v", name, err)
				continue
			}

			filePath := filepath.Join("json", name)
			log.Printf("[STARTUP] Loading stations page %d from file: %s", pageNum, filePath)
			content, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("[STARTUP] Error reading stations from file %s: %v", filePath, err)
				continue
			}

			contentString := string(content)

			nodeIdCount := strings.Count(contentString, "node_id")

			// Store the stations data in memory
			StoreJSONPageInMemory(pageNum, contentString, RequestTypeStationsPage, nodeIdCount)
		} else if strings.HasPrefix(name, "prices_page_") && strings.HasSuffix(name, ".json") {
			pageNumStr := strings.TrimSuffix(strings.TrimPrefix(name, "prices_page_"), ".json")
			pageNum, err := strconv.Atoi(pageNumStr)
			if err != nil {
				log.Printf("[STARTUP] Error parsing page number from filename %s: %v", name, err)
				continue
			}

			filePath := filepath.Join("json", name)
			log.Printf("[STARTUP] Loading prices page %d from file: %s", pageNum, filePath)
			content, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("[STARTUP] Error reading prices from file %s: %v", filePath, err)
				continue
			}

			contentString := string(content)

			nodeIdCount := strings.Count(contentString, "node_id")

			// Store the prices data in memory
			StoreJSONPageInMemory(pageNum, contentString, RequestTypePricesPage, nodeIdCount)

		}
	}
}
