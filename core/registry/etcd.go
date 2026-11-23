package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/davyxu/golog"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var log = golog.New("registry")

// etcdRegistry 基于 etcd 租约的实现
type etcdRegistry struct {
	client *clientv3.Client
	prefix string
	ttl    time.Duration
}

// NewEtcd 连接 etcd 并返回注册中心实现
func NewEtcd(cfg Config) (Registry, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("registry: endpoints required")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "/mgame"
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Second
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("registry etcd dial: %w", err)
	}
	return &etcdRegistry{client: cli, prefix: cfg.Prefix, ttl: cfg.TTL}, nil
}

// --- 注册（租约 + 后台续约） ---

func (e *etcdRegistry) Register(ctx context.Context, inst Instance) (Registration, error) {
	key := InstanceKey(e.prefix, inst.Service, inst.ID)
	b, err := json.Marshal(record{Addr: inst.Addr, Meta: inst.Meta})
	if err != nil {
		return nil, fmt.Errorf("registry marshal: %w", err)
	}
	val := string(b)

	leaseID, err := e.grantPut(ctx, key, val)
	if err != nil {
		return nil, err
	}

	kaCtx, cancel := context.WithCancel(context.Background())
	r := &leaseReg{e: e, key: key, val: val, cancel: cancel, done: make(chan struct{})}
	go r.loop(kaCtx, leaseID)
	return r, nil
}

// grantPut 申请租约并带租约写入实例键
func (e *etcdRegistry) grantPut(ctx context.Context, key, val string) (clientv3.LeaseID, error) {
	lease, err := e.client.Grant(ctx, int64(e.ttl/time.Second))
	if err != nil {
		return 0, fmt.Errorf("registry etcd grant: %w", err)
	}
	if _, err := e.client.Put(ctx, key, val, clientv3.WithLease(lease.ID)); err != nil {
		return 0, fmt.Errorf("registry etcd put: %w", err)
	}
	return lease.ID, nil
}

type leaseReg struct {
	e      *etcdRegistry
	key    string
	val    string
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
}

// loop 保持租约存活；续约通道关闭（租约过期/etcd 不可达）时自动重新注册
func (r *leaseReg) loop(ctx context.Context, leaseID clientv3.LeaseID) {
	defer close(r.done)

	for {
		if r.keepAliveOnce(ctx, leaseID) {
			return // ctx 取消 = 优雅下线
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		newLease, err := r.e.grantPut(ctx, r.key, r.val)
		if err != nil {
			log.Warnf("registry re-register failed key=%s: %v", r.key, err)
			continue
		}
		leaseID = newLease
		log.Infof("registry re-registered key=%s", r.key)
	}
}

// keepAliveOnce 续约直到通道关闭；返回 true 表示 ctx 取消（应退出）
func (r *leaseReg) keepAliveOnce(ctx context.Context, leaseID clientv3.LeaseID) bool {
	kaCh, err := r.e.client.KeepAlive(ctx, leaseID)
	if err != nil {
		log.Warnf("registry keepalive start failed key=%s: %v", r.key, err)
		return false
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case _, ok := <-kaCh:
			if !ok {
				log.Warnf("registry keepalive closed key=%s, re-registering", r.key)
				return false
			}
		}
	}
}

// Close 停止续约并删除实例键（进程退出/优雅下线时调用）
func (r *leaseReg) Close() error {
	r.once.Do(r.cancel)
	<-r.done

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := r.e.client.Delete(ctx, r.key)
	if err != nil {
		return fmt.Errorf("registry delete key=%s: %w", r.key, err)
	}
	log.Infof("registry deregistered key=%s", r.key)
	return nil
}

// --- 发现 ---

// Discover 查询 service 下全部实例
func (e *etcdRegistry) Discover(ctx context.Context, service string) ([]Instance, error) {
	resp, err := e.client.Get(ctx, ServicePrefix(e.prefix, service), clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("registry etcd get: %w", err)
	}
	return decodeInstances(service, resp), nil
}

// Watch 订阅 service 实例变化：先推一次全量，之后每次变更重列并推最新快照。
// 快照采用 latest-wins：消费者来不及处理时丢弃中间态。watch 断开自动重建。
func (e *etcdRegistry) Watch(ctx context.Context, service string) (<-chan []Instance, error) {
	prefix := ServicePrefix(e.prefix, service)

	init, err := e.Discover(ctx, service)
	if err != nil {
		return nil, err
	}

	out := make(chan []Instance, 1)
	send(ctx, out, init)

	go func() {
		defer close(out)
		for ctx.Err() == nil {
			wch := e.client.Watch(ctx, prefix, clientv3.WithPrefix())
			for resp := range wch {
				if resp.Err() != nil {
					break // watch 异常，下方重建
				}
				cur, err := e.Discover(ctx, service)
				if err != nil {
					log.Warnf("registry watch list failed service=%s: %v", service, err)
					continue
				}
				send(ctx, out, cur)
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return out, nil
}

// send latest-wins 投递快照
func send(ctx context.Context, out chan []Instance, insts []Instance) {
	select {
	case out <- insts:
		return
	case <-ctx.Done():
		return
	default:
	}
	// 缓冲已满：丢弃最旧的一份再投最新
	select {
	case <-out:
	default:
	}
	select {
	case out <- insts:
	case <-ctx.Done():
	}
}

func decodeInstances(service string, resp *clientv3.GetResponse) []Instance {
	out := make([]Instance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var rec record
		if err := json.Unmarshal(kv.Value, &rec); err != nil {
			log.Warnf("registry bad record key=%s: %v", kv.Key, err)
			continue
		}
		if rec.Addr == "" {
			continue
		}
		out = append(out, Instance{
			Service: service,
			ID:      path.Base(string(kv.Key)),
			Addr:    rec.Addr,
			Meta:    rec.Meta,
		})
	}
	return out
}

// Close 关闭 etcd 连接
func (e *etcdRegistry) Close() error {
	return e.client.Close()
}
