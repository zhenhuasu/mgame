package config

// Registry 服务注册与发现配置（各服务共用）
type Registry struct {
	// etcd 服务端地址列表，空 = 不启用（回退静态配置/不注册）
	Endpoints []string `yaml:"endpoints"`
	// 键前缀，默认 /mgame
	Prefix string `yaml:"prefix"`
	// 实例租约时长（秒），默认 10；进程崩溃后该时长内由 etcd 自动清理
	TTL int `yaml:"ttl"`
}

// Enabled 是否启用 etcd 注册发现
func (r *Registry) Enabled() bool {
	return len(r.Endpoints) > 0
}

// ApplyDefaults 填充默认值（load 时调用）
func (r *Registry) ApplyDefaults() {
	if r.Prefix == "" {
		r.Prefix = "/mgame"
	}
	if r.TTL == 0 {
		r.TTL = 10
	}
}
