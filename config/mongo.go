package config

// Mongo 各服务共用的 MongoDB 连接配置（当前 game 用户/角色持久化用）
type Mongo struct {
	// 连接串，如 mongodb://127.0.0.1:27017；空 = 未配置（game 启动即失败）
	URI string `yaml:"uri"`
	// 数据库名，默认 mgame
	Database string `yaml:"database"`
}

// Enabled 是否配置了 URI
func (m Mongo) Enabled() bool { return m.URI != "" }

// ApplyDefaults 填充默认值
func (m *Mongo) ApplyDefaults() {
	if m.Database == "" {
		m.Database = "mgame"
	}
}
