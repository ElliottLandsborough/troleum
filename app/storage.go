package main

import (
	"os"
	"strconv"
	"sync"
)

// Up to 100,000 stations expected (~8000 in the uk)
var stations = make([]Station, 0, 10000)
var stationsIndex = make(map[string]int, 10000)

// map of indexed json files already saved with their page numbers and datestamps
var savedStationsPages = make(map[int]string)
var savedPricesPages = make(map[int]string)

var savedPagesMutex sync.Mutex

// Mutex for thread-safe access to stations slice and index
var stationsMutex sync.Mutex

// Mutex for thread-safe access to prices slice and index
var pricesMutex sync.Mutex

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
	for pageNum := 1; ; pageNum++ {
		filePath := getStationsPageFilePath(pageNum)
		if fileExists(filePath) {
			// get the timestamp that the file was edited
			storeSavedPage(savedStationsPages, &savedPagesMutex, pageNum, filePath)
		} else {
			break
		}
	}

	for pageNum := 1; ; pageNum++ {
		filePath := getPricesPageFilePath(pageNum)
		if fileExists(filePath) {
			// get the timestamp that the file was edited
			storeSavedPage(savedPricesPages, &savedPagesMutex, pageNum, filePath)
		} else {
			break
		}
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

// Merges newStations into the global stations slice, avoiding duplicates based on NodeID
func mergeStations(newStations []Station) {
	for _, newStation := range newStations {
		if _, exists := stationsIndex[newStation.NodeID]; !exists {
			stationsIndex[newStation.NodeID] = len(stations)
			stations = append(stations, newStation)
		}
	}
}

/*
var prices = make([]PriceStation, 0, 100000)
var pricesIndex = make(map[string]int, 100000)

// Merges newPrices into the global prices slice, avoiding duplicates based on a composite key
func mergePrices(newPrices []PriceStation) {
	for _, newPrice := range newPrices {
		key := newPrice.NodeID + "|" + newPrice.FuelType + "|" + newPrice.DateTime
		if _, exists := pricesIndex[key]; !exists {
			pricesIndex[key] = len(prices)
			prices = append(prices, newPrice)
		}
	}
}
*/
