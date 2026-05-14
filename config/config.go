package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	UpstreamBaseURL string
	ListenAddr      string
	LogFile         string
	RouteMap        map[string]string
}

func Load() *Config {
	routeMap := parseRouteMap(getEnv("UPSTREAM_ROUTES", ""))

	return &Config{
		UpstreamBaseURL: getEnv("UPSTREAM_BASE_URL", "https://opencode.ai/zen"),
		ListenAddr:      getEnv("LISTEN_ADDR", ":8082"),
		LogFile:         getEnv("LOG_FILE", "./logs/proxy.log"),
		RouteMap:        routeMap,
	}
}

func parseRouteMap(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
