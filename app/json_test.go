package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSONPretty(t *testing.T) {
	w := httptest.NewRecorder()
	if err := writeJSONPretty(w, map[string]any{"ok": true}); err != nil {
		t.Fatalf("writeJSONPretty returned error: %v", err)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content type, got %q", ct)
	}
	if strings.TrimSpace(w.Body.String()) != "{\"ok\":true}" {
		t.Fatalf("unexpected JSON body: %q", w.Body.String())
	}

	w = httptest.NewRecorder()
	if err := writeJSONPretty(w, map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("expected marshal error for unsupported type")
	}
}

func TestFlexFloatUnmarshalJSON(t *testing.T) {
	var number FlexFloat
	if err := number.UnmarshalJSON([]byte(`12.34`)); err != nil {
		t.Fatalf("numeric unmarshal failed: %v", err)
	}
	if float64(number) != 12.34 {
		t.Fatalf("expected 12.34, got %v", number)
	}

	var strNum FlexFloat
	if err := strNum.UnmarshalJSON([]byte(`"56.78"`)); err != nil {
		t.Fatalf("string unmarshal failed: %v", err)
	}
	if float64(strNum) != 56.78 {
		t.Fatalf("expected 56.78, got %v", strNum)
	}

	var invalid FlexFloat
	if err := invalid.UnmarshalJSON([]byte(`"not-a-number"`)); err == nil {
		t.Fatal("expected error for invalid string float")
	}
}

func TestProcessJSONArray(t *testing.T) {
	direct := json.RawMessage(`[{"node_id":"station-1"}]`)
	stationsDirect, err := processJSONArray[Station](direct, 1, RequestTypeStationsPage)
	if err != nil {
		t.Fatalf("direct array parse failed: %v", err)
	}
	if len(stationsDirect) != 1 || stationsDirect[0].NodeID != "station-1" {
		t.Fatalf("unexpected direct parse result: %#v", stationsDirect)
	}

	wrapped := json.RawMessage(`{"success":true,"data":[{"node_id":"station-2"}]}`)
	stationsWrapped, err := processJSONArray[Station](wrapped, 2, RequestTypeStationsPage)
	if err != nil {
		t.Fatalf("wrapped parse failed: %v", err)
	}
	if len(stationsWrapped) != 1 || stationsWrapped[0].NodeID != "station-2" {
		t.Fatalf("unexpected wrapped parse result: %#v", stationsWrapped)
	}

	if _, err := processJSONArray[Station](json.RawMessage(``), 3, RequestTypeStationsPage); err == nil {
		t.Fatal("expected error for empty JSON payload")
	}
	if _, err := processJSONArray[Station](json.RawMessage(`{"broken":`), 4, RequestTypeStationsPage); err == nil {
		t.Fatal("expected error for malformed JSON payload")
	}
}

func TestSavePageJSON(t *testing.T) {
	tempDir := withTempWorkingDir(t)
	path, err := savePageJSON(`[{"ok":true}]`, 3, "stations")
	if err != nil {
		t.Fatalf("savePageJSON failed: %v", err)
	}
	wantPath := filepath.Join("json", "stations_page_3.json")
	if path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, path)
	}
	content, err := os.ReadFile(filepath.Join(tempDir, wantPath))
	if err != nil {
		t.Fatalf("reading saved file failed: %v", err)
	}
	if string(content) != `[{"ok":true}]` {
		t.Fatalf("unexpected saved content: %s", string(content))
	}
}

func TestLoadDataFromJSONFiles(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	tempDir := withTempWorkingDir(t)
	jsonDir := filepath.Join(tempDir, "json")
	if err := os.MkdirAll(jsonDir, 0o755); err != nil {
		t.Fatalf("mkdir json dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "stations_page_1.json"), []byte(testStationPageJSON), 0o600); err != nil {
		t.Fatalf("write stations page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "prices_page_1.json"), []byte(testPricePageJSON), 0o600); err != nil {
		t.Fatalf("write prices page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "stations_page_bad.json"), []byte(testStationPageJSON), 0o600); err != nil {
		t.Fatalf("write bad stations page: %v", err)
	}

	loadDataFromJSONFiles()

	stationsMutex.Lock()
	stationCount := len(stations)
	stationsMutex.Unlock()
	if stationCount != 2 {
		t.Fatalf("expected 2 stations loaded, got %d", stationCount)
	}

	priceStationsMutex.Lock()
	priceCount := len(priceStations)
	priceStationsMutex.Unlock()
	if priceCount != 2 {
		t.Fatalf("expected 2 price stations loaded, got %d", priceCount)
	}
}
