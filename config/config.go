package config

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
	UpstreamBaseURL   string
	ListenAddr        string
	LogFile           string
	LogLevel          string
	RouteMap          map[string]string
	SearchResultCount int
	SearchTimeout     int
	SearchBingURL     string
	SearchConcurrency int
	SearchRetryCount  int
	SearchBackend     string
	SearXNGBaseURL    string
	SearXNGSummarize  bool
	BlockWebSearch    bool
}

func Load() *Config {
	routeMap := parseRouteMap(getEnv("UPSTREAM_ROUTES", ""))

	defaultResultCount := getEnvInt("BING_SEARCH_RESULT_COUNT", 15)

	return &Config{
		UpstreamBaseURL:   getEnv("UPSTREAM_BASE_URL", "https://opencode.ai/zen"),
		ListenAddr:        getEnv("LISTEN_ADDR", ":8082"),
		LogFile:           getEnv("LOG_FILE", "./logs/proxy.log"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		RouteMap:          routeMap,
		SearchResultCount: defaultResultCount,
		SearchTimeout:     getEnvInt("BING_SEARCH_TIMEOUT", 30),
		SearchBingURL:     getEnv("BING_SEARCH_URL", "https://www.bing.com/search?q="),
		SearchConcurrency: getEnvInt("BING_SEARCH_CONCURRENCY", defaultResultCount),
		SearchRetryCount:  getEnvInt("SEARCH_RETRY_COUNT", 3),
		SearchBackend:     getEnv("SEARCH_BACKEND", "searxng"),
		SearXNGBaseURL:    getEnv("SEARXNG_BASE_URL", "http://localhost:8086"),
		SearXNGSummarize:  getEnv("SEARXNG_SUMMARIZE", "") == "true",
		BlockWebSearch:    getEnv("BLOCK_WEB_SEARCH", "true") == "true",
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

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultValue
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
