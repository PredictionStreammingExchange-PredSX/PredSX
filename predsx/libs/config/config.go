package config

import (
	"fmt"
	"os"
	"strconv"
)

// Interface defines the configuration methods.
type Interface interface {
	Get(key, fallback string) string
	GetInt(key string, fallback int) int
	GetBool(key string, fallback bool) bool
	Require(key string) (string, error)
}

type Config struct{}

func New() Interface {
	return &Config{}
}

func (c *Config) Get(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func (c *Config) GetInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		i, err := strconv.Atoi(value)
		if err == nil {
			return i
		}
	}
	return fallback
}

func (c *Config) GetBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(value)
		if err == nil {
			return b
		}
	}
	return fallback
}

func (c *Config) Require(key string) (string, error) {
	if value, ok := os.LookupEnv(key); ok {
		return value, nil
	}
	return "", fmt.Errorf("required environment variable %s is missing", key)
}

// Helper functions for package-level access (backwards compatibility)
func GetEnv(key, fallback string) string { return New().Get(key, fallback) }
func GetEnvInt(key string, fallback int) int { return New().GetInt(key, fallback) }
func GetEnvBool(key string, fallback bool) bool { return New().GetBool(key, fallback) }
