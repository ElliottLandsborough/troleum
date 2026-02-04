// a router or web handler or server that can return storeSavedPage in a human readable format online
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// Handler to return saved stations pages
func savedStationsHandler(w http.ResponseWriter, r *http.Request) {
	savedPagesMutex.Lock()
	defer savedPagesMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(savedStationsPages); err != nil {
		http.Error(w, "Failed to encode saved stations pages", http.StatusInternalServerError)
	}
}

// Handler to return saved prices pages
func savedPricesHandler(w http.ResponseWriter, r *http.Request) {
	savedPagesMutex.Lock()
	defer savedPagesMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(savedPricesPages); err != nil {
		http.Error(w, "Failed to encode saved prices pages", http.StatusInternalServerError)
	}
}

// Setup web server with routes for saved pages
func setupWebServer() {
	http.HandleFunc("/saved-stations", savedStationsHandler)
	http.HandleFunc("/saved-prices", savedPricesHandler)

	go func() {
		log.Println("Starting web server on :8080")
		if err := http.ListenAndServe("0.0.0.0:8080", nil); err != nil {
			log.Fatalf("Failed to start web server: %v", err)
		}
	}()
}
