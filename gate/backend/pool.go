package backend

import (
	"context"
	"sync"
	"time"

	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/gate/session"
)

// Pool gate → game 连接池：每个 game 地址一个 tcp.Connector（自动重连），
// RoundRobin 分配登录玩家，维护 uid → game 路由。
// 事件回调与客户端消息同在一个 EventQueue 内串行执行；加锁仅为防御未来跨 goroutine 调用。
type Pool struct {
	gateID string
	dis    Discovery
	hub    *session.Manager // 登录态：game→客户端回投时按 uid 查会话

	mu      sync.Mutex
	conns   map[string]cellnet.Peer // gameAddr → tcp.Connector
	rr      int
	routes  map[int64]*gameRoute // uid → 路由
	account map[int64]string     // uid → account（重连补发 PlayerOnline 用）
}

type gameRoute struct {
	addr    string
	account string
}

// New 创建连接池；hub 为共享登录态（gate 入口创建）
func New(gateID string, dis Discovery, hub *session.Manager) *Pool {
	return &Pool{
		gateID:  gateID,
		dis:     dis,
		hub:     hub,
		conns:   make(map[string]cellnet.Peer),
		routes:  make(map[int64]*gameRoute),
		account: make(map[int64]string),
	}
}

// Start 注册对内处理函数，订阅实例发现（首次回调即建 connector）。
// 事件在事件循环起动后才派发；register 须在其之前完成。
func (g *Pool) Start(ctx context.Context) {
	registerPoolHandlers(g)
	g.dis.Watch(ctx, g.reconcile)
}

// reconcile 按发现结果增删 game connector。
// 回调来自单一 watch goroutine（静态实现为 start 同步回调一次），无并发 reconcile。
func (g *Pool) reconcile(addrs []string) {
	want := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if a != "" {
			want[a] = true
		}
	}

	g.mu.Lock()
	var added, removed []string
	for a := range want {
		if _, ok := g.conns[a]; !ok {
			added = append(added, a)
		}
	}
	for a := range g.conns {
		if !want[a] {
			removed = append(removed, a)
		}
	}
	g.mu.Unlock()

	for _, addr := range added {
		g.connect(addr)
	}
	for _, addr := range removed {
		g.disconnect(addr)
	}
	if len(added) > 0 || len(removed) > 0 {
		log.Infof("game discovery updated total=%d added=%v removed=%v", len(want), added, removed)
	}
}

// connect 新建 game connector（仍复用统一 EventQueue 串行派发）
func (g *Pool) connect(addr string) {
	p := net.Connect(net.Options{
		Name:      addr,
		Addr:      addr,
		Reconnect: time.Second,
	})

	g.mu.Lock()
	if _, dup := g.conns[addr]; dup {
		g.mu.Unlock()
		if gp, ok := p.(cellnet.GenericPeer); ok {
			gp.Stop()
		}
		return
	}
	g.conns[addr] = p
	g.mu.Unlock()

	log.Infof("game connector start addr=%s", addr)
}

// disconnect 停止并移除 connector；断开事件由 onClosed 记录
func (g *Pool) disconnect(addr string) {
	g.mu.Lock()
	p, ok := g.conns[addr]
	delete(g.conns, addr)
	g.mu.Unlock()
	if !ok {
		return
	}
	if gp, ok2 := p.(cellnet.GenericPeer); ok2 {
		gp.Stop()
	}
	log.Infof("game connector stop addr=%s", addr)
}

// Assign 为 uid 轮询分配 game。返回 (新地址, 旧地址, 是否成功)；
// 旧地址非空且与新地址不同时，调用方需向旧实例发 PlayerOffline(reason=2)。
func (g *Pool) Assign(uid int64, account string) (string, string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	addr, ok := g.pickLocked()
	if !ok {
		return "", "", false
	}

	prev := ""
	if r, ok := g.routes[uid]; ok {
		prev = r.addr
	}
	g.routes[uid] = &gameRoute{addr: addr, account: account}
	g.account[uid] = account
	return addr, prev, true
}

// Unassign 移除路由，返回原地址（无路由返回 false）
func (g *Pool) Unassign(uid int64) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	r, ok := g.routes[uid]
	if !ok {
		return "", false
	}
	delete(g.routes, uid)
	delete(g.account, uid)
	return r.addr, true
}

// RouteOf 查询 uid 当前路由的 game 地址
func (g *Pool) RouteOf(uid int64) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	r, ok := g.routes[uid]
	if !ok {
		return "", false
	}
	return r.addr, true
}

// SendTo 向指定 game 地址发消息；未连接返回 false
func (g *Pool) SendTo(addr string, msg interface{}) bool {
	g.mu.Lock()
	p, ok := g.conns[addr]
	g.mu.Unlock()
	if !ok {
		return false
	}
	ses := p.(cellnet.TCPConnector).Session()
	if ses == nil {
		return false
	}
	ses.Send(msg)
	return true
}

// pickLocked 轮询选择一个已连接的 game
func (g *Pool) pickLocked() (string, bool) {
	addrs := g.dis.Addrs()
	if len(addrs) == 0 {
		return "", false
	}
	for i := 0; i < len(addrs); i++ {
		idx := (g.rr + i) % len(addrs)
		addr := addrs[idx]
		p, ok := g.conns[addr]
		if !ok {
			continue
		}
		ses := p.(cellnet.TCPConnector).Session()
		if ses == nil {
			continue
		}
		g.rr = (idx + 1) % len(addrs)
		return addr, true
	}
	return "", false
}

// uidsOn 路由到指定 game 的 uid 快照
func (g *Pool) uidsOn(addr string) []int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	var uids []int64
	for uid, r := range g.routes {
		if r.addr == addr {
			uids = append(uids, uid)
		}
	}
	return uids
}

func (g *Pool) accountOf(uid int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.account[uid]
}
