package registry

import (
	"context"
	"fmt"
	"sync"
)

// Static 进程内注册中心：无 etcd 时的联调/测试用实现。
// 注册与发现在同一进程内存中完成，不跨进程可见。
type Static struct {
	mu   sync.RWMutex
	data map[string]record // key = service/id
	subs map[string][]*sub // service → 订阅者
}

var _ Registry = (*Static)(nil)

// sub 一个 watch 订阅（once 保证关闭恰好一次）
type sub struct {
	ch   chan []Instance
	once sync.Once
}

func (s *sub) close() {
	s.once.Do(func() { close(s.ch) })
}

// NewStatic 创建内存注册中心
func NewStatic() *Static {
	return &Static{
		data: make(map[string]record),
		subs: make(map[string][]*sub),
	}
}

func (s *Static) Register(ctx context.Context, inst Instance) (Registration, error) {
	if inst.Service == "" || inst.ID == "" {
		return nil, fmt.Errorf("registry: service and id required")
	}
	key := inst.Service + "/" + inst.ID
	s.mu.Lock()
	s.data[key] = record{Addr: inst.Addr, Meta: inst.Meta}
	s.mu.Unlock()
	s.notify(inst.Service)

	return &staticReg{s: s, key: key, service: inst.Service}, nil
}

type staticReg struct {
	s       *Static
	key     string
	service string
	once    sync.Once
}

func (r *staticReg) Close() error {
	r.once.Do(func() {
		r.s.mu.Lock()
		delete(r.s.data, r.key)
		r.s.mu.Unlock()
		r.s.notify(r.service)
	})
	return nil
}

func (s *Static) Discover(ctx context.Context, service string) ([]Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Instance, 0)
	for key, rec := range s.data {
		if len(key) <= len(service) || key[:len(service)+1] != service+"/" {
			continue
		}
		out = append(out, Instance{
			Service: service,
			ID:      key[len(service)+1:],
			Addr:    rec.Addr,
			Meta:    rec.Meta,
		})
	}
	return out, nil
}

func (s *Static) Watch(ctx context.Context, service string) (<-chan []Instance, error) {
	b := &sub{ch: make(chan []Instance, 1)}

	s.mu.Lock()
	s.subs[service] = append(s.subs[service], b)
	s.mu.Unlock()

	if init, err := s.Discover(ctx, service); err == nil {
		send(ctx, b.ch, init)
	}

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		for i, cur := range s.subs[service] {
			if cur == b {
				s.subs[service] = append(s.subs[service][:i], s.subs[service][i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		b.close()
	}()
	return b.ch, nil
}

func (s *Static) notify(service string) {
	cur, err := s.Discover(context.Background(), service)
	if err != nil {
		return
	}
	s.mu.RLock()
	subs := append([]*sub(nil), s.subs[service]...)
	s.mu.RUnlock()

	for _, b := range subs {
		select {
		case b.ch <- cur:
		default:
		}
	}
}

func (s *Static) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, subs := range s.subs {
		for _, b := range subs {
			b.close()
		}
	}
	s.subs = make(map[string][]*sub)
	return nil
}
