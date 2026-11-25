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
	// 存储连接（Redis）：读缓存 + 全局 roleId 自增；addr 空 = 未配置（启动失败）
	Redis Redis `yaml:"redis"`
	// MongoDB 连接（user/role 唯一持久层）；uri 空 = 未配置（启动失败）
	Mongo Mongo `yaml:"mongo"`
	// 用户数据键配置（game 专属）
	User UserData `yaml:"user"`
}

// UserData 用户数据（game 专属）：Redis 缓存与角色开关
type UserData struct {
	// 缓存键前缀，默认 mgame:user:
	Prefix string `yaml:"prefix"`
	// 缓存过期秒数，0 = 不过期
	TTL int `yaml:"ttl"`
	// 角色列表为空时，是否允许 EnterGame(role_id=0) 由服务端默认创建角色；
	// false = 客户端须先 CreateRole 再 EnterGame
	AutoCreateRole bool `yaml:"autoCreateRole"`
}

// ApplyDefaults 填充默认值
func (u *UserData) ApplyDefaults() {
	if u.Prefix == "" {
		u.Prefix = "mgame:user:"
	}
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
	cfg.Mongo.ApplyDefaults()
	cfg.User.ApplyDefaults()
	if !cfg.Mongo.Enabled() {
		return nil, errf("mongo.uri required")
	}
	if !cfg.Redis.Enabled() {
		return nil, errf("redis.addr required")
	}
	if cfg.Advertise == "" {
		cfg.Advertise = cfg.Listen
	}
	if cfg.GameID == "" {
		cfg.GameID = fmt.Sprintf("game-%d", os.Getpid())
	}
	return cfg, nil
}
