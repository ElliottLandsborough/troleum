package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingResponseWriter struct {
	headers http.Header
}

func (f *failingResponseWriter) Header() http.Header {
	if f.headers == nil {
		f.headers = make(http.Header)
	}
	return f.headers
}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingResponseWriter) WriteHeader(int) {}

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

	fw := &failingResponseWriter{}
	if err := writeJSONPretty(fw, map[string]any{"ok": true}); err == nil {
		t.Fatal("expected write error to be returned")
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

	var wrongType FlexFloat
	if err := wrongType.UnmarshalJSON([]byte(`{"value":1}`)); err == nil {
		t.Fatal("expected error when value is neither number nor string")
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

func TestSavePageJSONDirectoryCreationFailure(t *testing.T) {
	withTempWorkingDir(t)

	if err := os.WriteFile("json", []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	if _, err := savePageJSON(`[{"ok":true}]`, 1, "prices"); err == nil {
		t.Fatal("expected savePageJSON to fail when json path is a file")
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
	if err := os.WriteFile(filepath.Join(jsonDir, "prices_page_bad.json"), []byte(testPricePageJSON), 0o600); err != nil {
		t.Fatalf("write bad prices page: %v", err)
	}
	if err := os.Mkdir(filepath.Join(jsonDir, "nested"), 0o755); err != nil {
		t.Fatalf("write nested dir: %v", err)
	}

	loadDataFromJSONFiles()

	savedStationsPagesMutex.Lock()
	savedStationsPageCount := len(savedStationsPages)
	savedStationsPagesMutex.Unlock()
	if savedStationsPageCount != 1 {
		t.Fatalf("expected 1 cached stations page loaded, got %d", savedStationsPageCount)
	}

	savedPricesPagesMutex.Lock()
	savedPricesPageCount := len(savedPricesPages)
	savedPricesPagesMutex.Unlock()
	if savedPricesPageCount != 1 {
		t.Fatalf("expected 1 cached prices page loaded, got %d", savedPricesPageCount)
	}

	priceStationsMutex.Lock()
	priceCount := len(priceStations)
	priceStationsMutex.Unlock()
	if priceCount != 2 {
		t.Fatalf("expected 2 price stations loaded, got %d", priceCount)
	}
}

func TestLoadDataFromJSONFilesUnreadableFiles(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	tempDir := withTempWorkingDir(t)
	jsonDir := filepath.Join(tempDir, "json")
	if err := os.MkdirAll(jsonDir, 0o755); err != nil {
		t.Fatalf("mkdir json dir: %v", err)
	}

	// Broken symlinks are stable cross-permission setups: ReadFile always fails.
	stationPath := filepath.Join(jsonDir, "stations_page_2.json")
	if err := os.Symlink(filepath.Join(jsonDir, "missing_stations_target.json"), stationPath); err != nil {
		t.Fatalf("create broken stations symlink: %v", err)
	}

	pricePath := filepath.Join(jsonDir, "prices_page_2.json")
	if err := os.Symlink(filepath.Join(jsonDir, "missing_prices_target.json"), pricePath); err != nil {
		t.Fatalf("create broken prices symlink: %v", err)
	}

	loadDataFromJSONFiles()

	savedStationsPagesMutex.Lock()
	stationPages := len(savedStationsPages)
	savedStationsPagesMutex.Unlock()
	if stationPages != 0 {
		t.Fatalf("expected no cached station pages from unreadable files, got %d", stationPages)
	}

	savedPricesPagesMutex.Lock()
	pricePages := len(savedPricesPages)
	savedPricesPagesMutex.Unlock()
	if pricePages != 0 {
		t.Fatalf("expected no cached price pages from unreadable files, got %d", pricePages)
	}
}

func TestLoadDataFromJSONFilesMissingDirectory(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)
	withTempWorkingDir(t)

	loadDataFromJSONFiles()

	savedStationsPagesMutex.Lock()
	stationPages := len(savedStationsPages)
	savedStationsPagesMutex.Unlock()
	if stationPages != 0 {
		t.Fatalf("expected no cached station pages, got %d", stationPages)
	}

	savedPricesPagesMutex.Lock()
	pricePages := len(savedPricesPages)
	savedPricesPagesMutex.Unlock()
	if pricePages != 0 {
		t.Fatalf("expected no cached price pages, got %d", pricePages)
	}
}
