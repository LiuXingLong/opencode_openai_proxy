package config

import (
	"os"
)

type Config struct {
	UpstreamBaseURL string
	ListenAddr      string
	LogFile         string
}

func Load() *Config {
	return &Config{
		UpstreamBaseURL: getEnv("UPSTREAM_BASE_URL", "https://opencode.ai/zen"),
		ListenAddr:      getEnv("LISTEN_ADDR", ":8082"),
		LogFile:         getEnv("LOG_FILE", "./logs/proxy.log"),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
