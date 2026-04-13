package main

import (
	"context"
	"testing"
	"time"
)

func TestNormalizeFuelPriceValue(t *testing.T) {
	tests := []struct {
		name      string
		raw       float64
		want      float64
		converted bool
	}{
		{name: "already in pence", raw: 152.9, want: 152.9, converted: false},
		{name: "pounds to pence", raw: 1.609, want: 160.9, converted: true},
		{name: "zero unchanged", raw: 0, want: 0, converted: false},
		{name: "negative unchanged", raw: -1, want: -1, converted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, converted := normalizeFuelPriceValue(tt.raw)
			if got != tt.want || converted != tt.converted {
				t.Fatalf("normalizeFuelPriceValue(%v) = (%v, %v), want (%v, %v)", tt.raw, got, converted, tt.want, tt.converted)
			}
		})
	}
}

func TestNormalizePriceStationsFuelPrices(t *testing.T) {
	input := []PriceStation{{
		NodeID:     "station-1",
		FuelPrices: []FuelPrice{{FuelType: "E10", Price: 1.609}, {FuelType: "DIESEL", Price: 152.9}},
	}}

	converted := normalizePriceStationsFuelPrices(input)
	if converted != 1 {
		t.Fatalf("expected 1 conversion, got %d", converted)
	}
	if input[0].FuelPrices[0].Price != 160.9 {
		t.Fatalf("expected converted price 160.9, got %v", input[0].FuelPrices[0].Price)
	}
}

func TestUpdateFuelTypesCacheAndGetCachedFuelTypes(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	priceStationsMutex.Lock()
	priceStations = []PriceStation{
		{NodeID: "1", FuelPrices: []FuelPrice{{FuelType: "E10"}, {FuelType: "DIESEL"}}},
		{NodeID: "2", FuelPrices: []FuelPrice{{FuelType: "E10"}, {FuelType: "PREMIUM"}}},
	}
	priceStationsMutex.Unlock()

	updateFuelTypesCache()
	fuelTypes := getCachedFuelTypes()
	if len(fuelTypes) != 3 {
		t.Fatalf("expected 3 unique fuel types, got %d (%v)", len(fuelTypes), fuelTypes)
	}

	fuelTypes[0] = "MUTATED"
	fresh := getCachedFuelTypes()
	for _, value := range fresh {
		if value == "MUTATED" {
			t.Fatal("expected getCachedFuelTypes to return a copy")
		}
	}
}

func TestFilterStationsByFuelType(t *testing.T) {
	stationsInput := []Station{
		{NodeID: "1", FuelTypes: []string{"E10", "DIESEL"}},
		{NodeID: "2", FuelTypes: []string{"DIESEL"}},
		{NodeID: "3", FuelTypes: []string{"E5"}},
	}

	filtered := filterStationsByFuelType(stationsInput, "DIESEL")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 stations with DIESEL, got %d", len(filtered))
	}
	if filterStationsByFuelType(stationsInput, "") == nil || len(filterStationsByFuelType(stationsInput, "")) != 3 {
		t.Fatal("expected empty fuel type to return original stations")
	}
}

func TestContinuousUpdateCachedFuelTypesStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	continuousUpdateCachedFuelTypes(ctx)
}

func TestContinuousFetchPricesStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rateLimiter := time.NewTicker(time.Hour)
	defer rateLimiter.Stop()

	continuousFetchPrices(ctx, nil, rateLimiter)
}
