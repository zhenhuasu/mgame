package config

// Redis 各服务共用的 Redis 连接配置（读缓存 / 全局自增键）。
// 键前缀/过期由各业务自行约定（如 game 缓存见 UserData）。
type Redis struct {
	// 连接地址 host:port；空 = 未配置（game 启动即失败）
	Addr string `yaml:"addr"`
	// 密码（无则留空）
	Password string `yaml:"password"`
	// 数据库序号
	DB int `yaml:"db"`
}

// Enabled 是否启用 Redis（Addr 非空）
func (r Redis) Enabled() bool { return r.Addr != "" }
