// Package registry 统一的服务注册与发现：
// 服务启动后用 Register 向 etcd 注册自身（租约+续约），其他服务用
// Discover/Watch 获取实例列表。endpoints 为空时由调用方回退静态配置。
package registry

import (
	"context"
	"path"
	"time"

	"github.com/zhenhuasu/mgame/config"
)

// 预置服务名（后续服务自行扩展）
const (
	ServiceGate  = "gate"
	ServiceGame  = "game"
	ServiceGuild = "guild"
	ServiceRank  = "rank"
)

// Instance 注册的服务实例
type Instance struct {
	Service string            // 服务名，如 gate/game
	ID      string            // 实例唯一 ID，如 gate-1234
	Addr    string            // 可拨号地址 host:port
	Meta    map[string]string // 附加信息，如 protocol
}

// Registration 一次注册的句柄，Close 撤销租约并停止续约
type Registration interface {
	Close() error
}

// Registry 服务注册与发现
type Registry interface {
	// Register 注册实例并后台续约；ctx 仅约束首次注册，不随其取消而注销
	Register(ctx context.Context, inst Instance) (Registration, error)
	// Discover 一次性查询 service 下全部实例
	Discover(ctx context.Context, service string) ([]Instance, error)
	// Watch 订阅 service 实例变化：先推一次全量，后续变更推最新快照（latest-wins）
	Watch(ctx context.Context, service string) (<-chan []Instance, error)
	// Close 释放底层连接
	Close() error
}

// Config etcd 注册中心配置
type Config struct {
	Endpoints []string
	Prefix    string // 默认 /mgame
	TTL       time.Duration
}

// ServicePrefix service 下实例键的前缀：{prefix}/services/{service}/
func ServicePrefix(prefix, service string) string {
	return path.Join(prefix, "services", service) + "/"
}

// InstanceKey 实例键
func InstanceKey(prefix, service, id string) string {
	return path.Join(prefix, "services", service, id)
}

// NewFromConfig 按配置创建注册中心；endpoints 为空返回 (nil, nil)，调用方回退静态配置。
// 服务启动时调用一次，返回的 Registry 由调用方在退出时 Close。
func NewFromConfig(rc config.Registry) (Registry, error) {
	if !rc.Enabled() {
		return nil, nil
	}
	return NewEtcd(Config{
		Endpoints: rc.Endpoints,
		Prefix:    rc.Prefix,
		TTL:       time.Duration(rc.TTL) * time.Second,
	})
}

// record etcd value 结构
type record struct {
	Addr string            `json:"addr"`
	Meta map[string]string `json:"meta,omitempty"`
}
