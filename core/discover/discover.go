// Package discover 通用后端实例发现：注册中心 watch（etcD）与静态列表两种实现。
// gate（发现 game）与 game（发现 guild）共用。
package discover

import (
	"context"
	"sync"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/registry"
)

// log 独立日志名（net.Init 通过 LogNames 纳入 LogLevel）
var log = golog.New("discover")

// Discovery 后端实例发现
type Discovery interface {
	// Addrs 当前实例地址快照
	Addrs() []string
	// Watch 订阅地址变化：注册中心实现先推一次全量（失败时回退静态地址），
	// 静态实现立即回调一次。返回取消函数。
	Watch(ctx context.Context, cb func([]string)) (cancel func())
}

// Static 静态地址列表（配置未启用注册中心时回退）
type Static []string

// Addrs 返回地址快照
func (s Static) Addrs() []string { return append([]string(nil), s...) }

// Watch 立即回调一次全量地址
func (s Static) Watch(ctx context.Context, cb func([]string)) (cancel func()) {
	if cb != nil {
		cb(s.Addrs())
	}
	return func() {}
}

// Registry 基于注册中心的服务发现，内部维护地址快照（latest-wins）。
// watch 启动失败（etcd 不可达）时回退 fallback 静态地址。
type Registry struct {
	reg      registry.Registry
	service  string
	fallback Discovery

	mu    sync.RWMutex
	addrs []string
}

// NewRegistry 创建注册中心发现；fallback 用于 watch 启动失败时兜底
func NewRegistry(reg registry.Registry, service string, fallback Discovery) *Registry {
	return &Registry{reg: reg, service: service, fallback: fallback}
}

// Addrs 返回地址快照
func (d *Registry) Addrs() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return append([]string(nil), d.addrs...)
}

func (d *Registry) setAddrs(addrs []string) {
	d.mu.Lock()
	d.addrs = append([]string(nil), addrs...)
	d.mu.Unlock()
}

// Watch 订阅实例变化
func (d *Registry) Watch(ctx context.Context, cb func([]string)) (cancel func()) {
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
			d.setAddrs(instanceAddrs(insts))
			if cb != nil {
				cb(d.Addrs())
			}
		}
	}()
	return cancelWatch
}

func instanceAddrs(insts []registry.Instance) []string {
	out := make([]string, 0, len(insts))
	for _, inst := range insts {
		out = append(out, inst.Addr)
	}
	return out
}
