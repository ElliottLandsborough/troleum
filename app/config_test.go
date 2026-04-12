package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	tempDir := withTempWorkingDir(t)
	filePath := filepath.Join(tempDir, ".env.test")
	content := strings.Join([]string{
		"# comment",
		"",
		"FOO=bar",
		"SPACED = value with spaces ",
		"MALFORMED_LINE",
	}, "\n")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("FOO", "")
	t.Setenv("SPACED", "")
	if err := loadDotEnv(filePath); err != nil {
		t.Fatalf("loadDotEnv failed: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Fatalf("expected FOO=bar, got %q", got)
	}
	if got := os.Getenv("SPACED"); got != "value with spaces" {
		t.Fatalf("expected SPACED to be trimmed, got %q", got)
	}
}

func TestLoadConfigAndMustEnv(t *testing.T) {
	t.Setenv("OAUTH_CLIENT_ID", "client-id")
	t.Setenv("OAUTH_CLIENT_SECRET", "secret")

	cfg := LoadConfig()
	if cfg.ClientID != "client-id" || cfg.ClientSecret != "secret" {
		t.Fatalf("unexpected config loaded: %#v", cfg)
	}
	if got := mustEnv("OAUTH_CLIENT_ID"); got != "client-id" {
		t.Fatalf("expected mustEnv to return client-id, got %q", got)
	}
}
