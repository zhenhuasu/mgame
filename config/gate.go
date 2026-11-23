package config

import (
	"fmt"
	"os"
)

// Gate 网关服务配置（config/gate.yaml）
type Gate struct {
	// 监听地址，如 0.0.0.0:8801
	Listen string `yaml:"listen"`
	// WebSocket 路径，如 /ws
	Path string `yaml:"path"`
	// 传输协议: ws | wss
	Protocol string `yaml:"protocol"`
	// wss 证书（protocol=wss 时必填）
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
	// 日志级别: debug | info | warn | error（空 = debug）
	// 同时作用于业务日志与 cellnet 内部日志（gorillaws/gorillawsproc）
	LogLevel string `yaml:"logLevel"`
	// gate 实例标识（注册到 game；空 = 进程启动时生成随机值）
	GateID string `yaml:"gateId"`
	// game 服务地址列表（后续可由 etcd 发现替换）
	GameServers []string `yaml:"gameServers"`
	// 客户端鉴权配置
	Auth Auth `yaml:"auth"`
	// gate↔game 共享密钥（RegisterGate HMAC；空 = 不校验，仅联调）
	GateSecret string `yaml:"gateSecret"`
	// 对外可达地址（注册到 etcd 用，host:port 或 ws://host:port/path；空 = 回退 listen）
	Advertise string `yaml:"advertise"`
	// 服务注册与发现（etcd），空 endpoints = 不启用
	Registry Registry `yaml:"registry"`
}

// Auth 客户端 token 鉴权配置
type Auth struct {
	// 校验模式: jwt | dev（空 = dev）
	Mode string `yaml:"mode"`
	// HS256 密钥，mode=jwt 时必填
	JWTSecret string `yaml:"jwtSecret"`
	// 会话有效期（秒），登录后超过该时长无活动则踢下线；0 = 不过期
	TokenTTL int `yaml:"tokenTTL"`
}

// LoadGate 加载网关配置
func LoadGate(path string) (*Gate, error) {
	cfg := &Gate{}
	if err := LoadYAML(path, cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (g *Gate) validate() error {
	if g.Listen == "" {
		g.Listen = "0.0.0.0:8801"
	}
	if g.Path == "" {
		g.Path = "/ws"
	}
	if g.Protocol == "" {
		g.Protocol = "ws"
	}
	switch g.Protocol {
	case "ws":
	case "wss":
		if g.CertFile == "" || g.KeyFile == "" {
			return errf("protocol=wss requires certFile and keyFile")
		}
	default:
		return errf("protocol must be ws or wss, got %q", g.Protocol)
	}
	if g.LogLevel == "" {
		g.LogLevel = "debug"
	}
	switch g.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return errf("logLevel must be debug|info|warn|error, got %q", g.LogLevel)
	}
	if g.GateID == "" {
		g.GateID = fmt.Sprintf("gate-%d", os.Getpid())
	}
	if g.Auth.Mode == "" {
		g.Auth.Mode = "dev"
	}
	switch g.Auth.Mode {
	case "dev":
	case "jwt":
		if g.Auth.JWTSecret == "" {
			return errf("auth.mode=jwt requires auth.jwtSecret")
		}
	default:
		return errf("auth.mode must be dev|jwt, got %q", g.Auth.Mode)
	}
	if g.Auth.TokenTTL < 0 {
		return errf("auth.tokenTTL must be >= 0, got %d", g.Auth.TokenTTL)
	}
	g.Registry.ApplyDefaults()
	if g.Advertise == "" {
		g.Advertise = g.Listen
	}
	return nil
}

// Addr 返回 cellnet 用的完整地址 scheme://host:port/path
func (g *Gate) Addr() string {
	scheme := "ws"
	if g.Protocol == "wss" {
		scheme = "wss"
	}
	return scheme + "://" + g.Listen + g.Path
}
