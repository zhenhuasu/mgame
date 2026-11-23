package backend

import (
	"context"
	"sync"

	"github.com/zhenhuasu/mgame/core/registry"
)

// Discovery 上游服务（game）实例发现：注册中心 watch 与静态配置两种实现。
type Discovery interface {
	// Addrs 当前实例地址快照
	Addrs() []string
	// Watch 订阅地址变化：注册中心实现先推一次全量（失败时回退静态地址），
	// 静态实现立即回调一次。返回取消函数。
	Watch(ctx context.Context, cb func([]string)) (cancel func())
}

// StaticDiscovery 静态地址列表（config.Gate.GameServers，registry 未启用时回退）
type StaticDiscovery []string

func (s StaticDiscovery) Addrs() []string { return append([]string(nil), s...) }

func (s StaticDiscovery) Watch(ctx context.Context, cb func([]string)) (cancel func()) {
	if cb != nil {
		cb(s.Addrs())
	}
	return func() {}
}

// RegistryDiscovery 基于注册中心的服务发现，内部维护地址快照（latest-wins）。
// watch 启动失败（etcd 不可达）时回退 fallback 静态地址。
type RegistryDiscovery struct {
	reg      registry.Registry
	service  string
	fallback Discovery

	mu    sync.RWMutex
	addrs []string
}

// NewRegistryDiscovery 创建注册中心发现；fallback 用于 watch 启动失败时兜底
func NewRegistryDiscovery(reg registry.Registry, service string, fallback Discovery) *RegistryDiscovery {
	return &RegistryDiscovery{reg: reg, service: service, fallback: fallback}
}

func (d *RegistryDiscovery) Addrs() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return append([]string(nil), d.addrs...)
}

func (d *RegistryDiscovery) setAddrs(addrs []string) {
	d.mu.Lock()
	d.addrs = append([]string(nil), addrs...)
	d.mu.Unlock()
}

func (d *RegistryDiscovery) Watch(ctx context.Context, cb func([]string)) (cancel func()) {
	wctx, cancelWatch := context.WithCancel(ctx)

	ch, err := d.reg.Watch(wctx, d.service)
	if err != nil {
		log.Warnf("discovery watch failed service=%s, fallback to static: %v", d.service, err)
		if d.fallback != nil {
			d.setAddrs(d.fallback.Addrs())
		}
		if cb != nil {
			cb(d.Addrs())
		}
		return cancelWatch
	}

	go func() {
		for insts := range ch {
			d.setAddrs(instancesAddrs(insts))
			if cb != nil {
				cb(d.Addrs())
			}
		}
	}()
	return cancelWatch
}

func instancesAddrs(insts []registry.Instance) []string {
	out := make([]string, 0, len(insts))
	for _, inst := range insts {
		out = append(out, inst.Addr)
	}
	return out
}
