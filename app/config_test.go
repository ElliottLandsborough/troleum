package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestLoadConfigAndMustEnv(t *testing.T) {
	t.Setenv("OAUTH_CLIENT_ID", "client-id")
	t.Setenv("OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("GOVAPI_ENABLED", "true")

	cfg := LoadConfig()
	if cfg.ClientID != "client-id" || cfg.ClientSecret != "secret" {
		t.Fatalf("unexpected config loaded: %#v", cfg)
	}
	if got := mustEnv("OAUTH_CLIENT_ID"); got != "client-id" {
		t.Fatalf("expected mustEnv to return client-id, got %q", got)
	}
}

func TestMustEnvMissingFatal(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		_ = mustEnv("MISSING_ENV_FOR_TEST")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMustEnvMissingFatal")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "MISSING_ENV_FOR_TEST=")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected subprocess to exit with non-zero status")
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected ExitError, got %T (%v)", err, err)
	}
}
