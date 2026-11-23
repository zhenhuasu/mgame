package config

import (
	"fmt"
	"os"
)

// Game 游戏服务配置（config/game.yaml）
type Game struct {
	// gate 连入的监听地址，如 127.0.0.1:8901
	Listen string `yaml:"listen"`
	// 日志级别: debug | info | warn | error（空 = debug）
	LogLevel string `yaml:"logLevel"`
	// gate↔game 共享密钥（RegisterGate HMAC；空 = 不校验，仅联调）
	GateSecret string `yaml:"gateSecret"`
	// 实例标识（注册到 etcd；空 = gate-<pid> 风格自动生成）
	GameID string `yaml:"gameId"`
	// 对外可达地址 host:port（注册到 etcd 用；空 = 回退 listen）
	Advertise string `yaml:"advertise"`
	// 服务注册与发现（etcd），空 endpoints = 不启用
	Registry Registry `yaml:"registry"`
}

// LoadGame 加载游戏服务配置
func LoadGame(path string) (*Game, error) {
	cfg := &Game{}
	if err := LoadYAML(path, cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8901"
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
	if cfg.Advertise == "" {
		cfg.Advertise = cfg.Listen
	}
	if cfg.GameID == "" {
		cfg.GameID = fmt.Sprintf("game-%d", os.Getpid())
	}
	return cfg, nil
}
