package main

import (
	"bufio"
	"log"
	"os"
	"strings"
)

type Config struct {
	TokenURL      string
	ClientID      string
	ClientSecret  string
	Scope         string
	GovAPIEnabled bool
}

func LoadConfig() Config {
	return Config{
		ClientID:      mustEnv("OAUTH_CLIENT_ID"),
		ClientSecret:  mustEnv("OAUTH_CLIENT_SECRET"),
		GovAPIEnabled: parseBoolEnv(mustEnv("GOVAPI_ENABLED")),
	}
}

// mustEnv ensures an environment variable is set
func mustEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return val
}

func loadDotEnv(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// skip empty lines or comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue // skip malformed lines
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		os.Setenv(key, value)
	}

	return scanner.Err()
}

// parseBoolEnv parses a string as a bool (1, true, yes, on = true; 0, false, no, off = false)
func parseBoolEnv(val string) bool {
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Fatalf("invalid boolean value for env var: %q", val)
		return false
	}
}
