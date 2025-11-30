package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
)

type LogConfig struct {
	Level     slog.Level `yaml:"level"`
	Format    string     `yaml:"format"`
	AddSource bool       `yaml:"add_source"`
	NoColor   bool       `yaml:"no_color"`
}

type CacheConfig struct {
	Enabled       bool   `yaml:"enabled"`
	CacheDir      string `yaml:"cache_dir"`
	CacheMaxLimit int64  `yaml:"cache_max_limit"`
}

type Config struct {
	Ipv6Subnet      string      `yaml:"ipv6_subnet"`
	MaxVisitorCount int         `yaml:"max_visitor_count"`
	RequestTimeout  int         `yaml:"request_timeout"`
	ServerAddr      string      `yaml:"server_addr"`
	Logging         LogConfig   `yaml:"logging"`
	Caching         CacheConfig `yaml:"caching"`
}

func (cfg Config) String() string {
	return fmt.Sprintf(
		"Config{Ipv6Subnet: %s, MaxVisitorCount: %d, RequestTimeout: %d, ServerAddr: %s, Logging: %+v}",
		cfg.Ipv6Subnet,
		cfg.MaxVisitorCount,
		cfg.RequestTimeout,
		cfg.ServerAddr,
		cfg.Logging,
	)
}

func ReadConfig(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}

	if cfg.Caching.Enabled && cfg.Caching.CacheDir == "" {
		cfg.Caching.CacheDir = "./cache.db"
	}

	if cfg.Caching.Enabled && cfg.Caching.CacheMaxLimit == 0 {
		cfg.Caching.CacheMaxLimit = -1 // no limit
	}

	if cfg.MaxVisitorCount <= 0 {
		cfg.MaxVisitorCount = 2
	}

	if cfg.ServerAddr == "" {
		cfg.ServerAddr = ":8080"
	}

	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10
	}

	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}

	return &cfg, nil
}
