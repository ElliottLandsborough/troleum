package main

// Up to 100,000 stations expected (~8000 in the uk)
var stations = make([]Station, 0, 100000)
var stationsIndex = make(map[string]int, 100000)

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
