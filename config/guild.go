package config

import (
	"fmt"
	"os"
)

// Guild 公会服务配置（config/guild.yaml）
type Guild struct {
	// game 连入的监听地址，如 127.0.0.1:8903
	Listen string `yaml:"listen"`
	// 日志级别: debug | info | warn | error（空 = debug）
	LogLevel string `yaml:"logLevel"`
	// game↔guild 共享密钥（RegisterGame HMAC；空 = 不校验，仅联调）
	GuildSecret string `yaml:"guildSecret"`
	// 实例标识（注册到 etcd；空 = guild-<pid> 自动生成）
	GuildID string `yaml:"guildId"`
	// 对外可达地址 host:port（注册到 etcd 用；空 = 回退 listen）
	Advertise string `yaml:"advertise"`
	// 服务注册与发现（etcd），空 endpoints = 不启用
	Registry Registry `yaml:"registry"`
	// 存储连接（Redis）：全局 guildId 自增；addr 空 = 未配置（启动失败）
	Redis Redis `yaml:"redis"`
	// MongoDB 连接（guilds/members/invites 唯一持久层）；uri 空 = 未配置（启动失败）
	Mongo Mongo `yaml:"mongo"`
	// 公会业务参数
	Guild GuildData `yaml:"guild"`
}

// GuildData 公会业务参数
type GuildData struct {
	// 公会成员上限
	MaxMembers int `yaml:"maxMembers"`
	// 公会名最大长度（去空格后）
	NameMaxLen int `yaml:"nameMaxLen"`
	// 公告最大长度
	NoticeMaxLen int `yaml:"noticeMaxLen"`
}

// ApplyDefaults 填充默认值
func (g *GuildData) ApplyDefaults() {
	if g.MaxMembers <= 0 {
		g.MaxMembers = 50
	}
	if g.NameMaxLen <= 0 {
		g.NameMaxLen = 24
	}
	if g.NoticeMaxLen <= 0 {
		g.NoticeMaxLen = 200
	}
}

// LoadGuild 加载公会服务配置
func LoadGuild(path string) (*Guild, error) {
	cfg := &Guild{}
	if err := LoadYAML(path, cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8903"
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
	cfg.Guild.ApplyDefaults()
	if !cfg.Mongo.Enabled() {
		return nil, errf("mongo.uri required")
	}
	if !cfg.Redis.Enabled() {
		return nil, errf("redis.addr required")
	}
	if cfg.Advertise == "" {
		cfg.Advertise = cfg.Listen
	}
	if cfg.GuildID == "" {
		cfg.GuildID = fmt.Sprintf("guild-%d", os.Getpid())
	}
	return cfg, nil
}
