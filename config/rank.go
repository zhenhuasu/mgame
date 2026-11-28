package config

import (
	"fmt"
	"os"
)

type Rank struct {
	Listen      string   `yaml:"listen"`
	LogLevel    string   `yaml:"logLevel"`
	RankSecret  string   `yaml:"rankSecret"`
	RankID      string   `yaml:"rankId"`
	Advertise   string   `yaml:"advertise"`
	Registry    Registry `yaml:"registry"`
	Redis       Redis    `yaml:"redis"`
	RankOptions RankData `yaml:"rank"`
}

type RankData struct {
	DefaultLimit int `yaml:"defaultLimit"`
	MaxLimit     int `yaml:"maxLimit"`
}

func (r *RankData) ApplyDefaults() {
	if r.DefaultLimit <= 0 {
		r.DefaultLimit = 20
	}
	if r.MaxLimit <= 0 {
		r.MaxLimit = 100
	}
	if r.MaxLimit < r.DefaultLimit {
		r.MaxLimit = r.DefaultLimit
	}
}

func LoadRank(path string) (*Rank, error) {
	cfg := &Rank{}
	if err := LoadYAML(path, cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8904"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "debug"
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, errf("logLevel must be debug|info|warn|error, got %q", cfg.LogLevel)
	}
	cfg.Registry.ApplyDefaults()
	cfg.RankOptions.ApplyDefaults()
	if !cfg.Redis.Enabled() {
		return nil, errf("redis.addr required")
	}
	if cfg.Advertise == "" {
		cfg.Advertise = cfg.Listen
	}
	if cfg.RankID == "" {
		cfg.RankID = fmt.Sprintf("rank-%d", os.Getpid())
	}
	return cfg, nil
}
