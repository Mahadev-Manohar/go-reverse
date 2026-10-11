
package main

import (
	"fmt"
	"os"
	"strings"
	"bytes"
	"math"
	"net/url"
	"strconv"

	"gopkg.in/yaml.v3"
)


type Config struct {
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`

	Backends []BackendConfig `yaml:"backends"`

	RateLimit struct {
		Enabled           bool    `yaml:"enabled"`
		RequestsPerSecond float64 `yaml:"requests_per_second"`
		Burst             float64 `yaml:"burst"`
	} `yaml:"rate_limit"`
}

type BackendConfig struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}


func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config Config

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Environment variables override YAML values.
	if value, ok := os.LookupEnv("GOREVERSE_PORT"); ok {
		port, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid GOREVERSE_PORT: %w", err)
		}
		config.Server.Port = port
	}

	if value, ok := os.LookupEnv("GOREVERSE_RATE_LIMIT_ENABLED"); ok {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid GOREVERSE_RATE_LIMIT_ENABLED: %w", err,
			)
		}
		config.RateLimit.Enabled = enabled
	}

	if value, ok := os.LookupEnv("GOREVERSE_RATE_LIMIT_RPS"); ok {
		rate, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GOREVERSE_RATE_LIMIT_RPS: %w", err)
		}
		config.RateLimit.RequestsPerSecond = rate
	}

	if value, ok := os.LookupEnv("GOREVERSE_RATE_LIMIT_BURST"); ok {
		burst, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid GOREVERSE_RATE_LIMIT_BURST: %w", err,
			)
		}
		config.RateLimit.Burst = burst
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}


func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}

	if len(c.Backends) == 0 {
		return fmt.Errorf("at least one backend is required")
	}

	seenNames := make(map[string]bool)
	seenURLs := make(map[string]bool)

	for _, backend := range c.Backends {
		name := strings.TrimSpace(backend.Name)
		if name == "" {
			return fmt.Errorf("backend name cannot be empty")
		}
		if seenNames[name] {
			return fmt.Errorf("duplicate backend name: %s", name)
		}
		seenNames[name] = true

		rawURL := strings.TrimSpace(backend.URL)
		if rawURL == "" {
			return fmt.Errorf("backend URL cannot be empty: %s", name)
		}

		parsed, err := url.ParseRequestURI(rawURL)
		if err != nil ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Hostname() == "" {
			return fmt.Errorf("invalid HTTP(S) URL for backend %s: %s", name, rawURL)
		}

		if seenURLs[rawURL] {
			return fmt.Errorf("duplicate backend URL: %s", rawURL)
		}
		seenURLs[rawURL] = true
	}

	if c.RateLimit.Enabled {
		rate := c.RateLimit.RequestsPerSecond
		burst := c.RateLimit.Burst

		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return fmt.Errorf("rate_limit.requests_per_second must be finite and positive")
		}

		if burst < 1 || math.IsNaN(burst) || math.IsInf(burst, 0) {
			return fmt.Errorf("rate_limit.burst must be finite and at least 1")
		}
	}

	return nil
}
