//go:build debug

package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONPrettyDebugMode(t *testing.T) {
	w := httptest.NewRecorder()
	if err := writeJSONPretty(w, map[string]any{"ok": true, "name": "station"}); err != nil {
		t.Fatalf("writeJSONPretty returned error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "\n") {
		t.Fatalf("expected pretty-printed JSON with newlines in debug mode, got %q", body)
	}
	if !strings.Contains(body, "  \"ok\"") {
		t.Fatalf("expected indented field in pretty JSON, got %q", body)
	}
}
