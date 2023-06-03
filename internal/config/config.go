package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Auth    AuthConfig    `yaml:"auth"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type StorageConfig struct {
	DataDir string `yaml:"dataDir"`
}

type AuthConfig struct {
	Tokens []string `yaml:"tokens"`
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		Storage: StorageConfig{DataDir: "./data"},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(cfg.Auth.Tokens) == 0 {
		return nil, fmt.Errorf("no auth tokens configured")
	}

	return cfg, nil
}
