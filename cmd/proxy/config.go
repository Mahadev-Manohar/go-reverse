
package main

import (
	"fmt"
	"os"
	"strings"

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

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
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
		if strings.TrimSpace(backend.Name) == "" {
			return fmt.Errorf("backend name cannot be empty")
		}

		if seenNames[backend.Name] {
			return fmt.Errorf("duplicate backend name: %s", backend.Name)
		}
		seenNames[backend.Name] = true

		if strings.TrimSpace(backend.URL) == "" {
			return fmt.Errorf("backend URL cannot be empty: %s", backend.Name)
		}

		if seenURLs[backend.URL] {
			return fmt.Errorf("duplicate backend URL: %s", backend.URL)
		}
		seenURLs[backend.URL] = true

		if !strings.HasPrefix(backend.URL, "http://") &&
			!strings.HasPrefix(backend.URL, "https://") {
			return fmt.Errorf(
				"backend %s must use an http:// or https:// URL",
				backend.Name,
			)
		}
	}

	if c.RateLimit.Enabled {
		if c.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("rate_limit.requests_per_second must be positive")
		}

		if c.RateLimit.Burst < 1 {
			return fmt.Errorf("rate_limit.burst must be at least 1")
		}
	}

	return nil
}
