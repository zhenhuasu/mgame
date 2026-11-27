package guildclient

import (
	"context"
	"sync"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/discover"
	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("game.guildclient")

// Forwarder 下行投递回调（game handler 实现：GuildS2C→客户端、GuildNotify→在线角色）
type Forwarder interface {
	ForwardGuildS2C(uid int64, msgID uint32, payload []byte)
	NotifyGuild(roleID int64, msgID uint32, payload []byte)
}

// Pool game→guild 连接池：每个 guild 地址一个 tcp.Connector（自动重连），
// 发送时轮询任一就绪连接（公会数据在 Mongo，实例间无状态差异）。
type Pool struct {
	gameID string
	dis    discover.Discovery
	fwd    Forwarder

	mu    sync.Mutex
	conns map[string]cellnet.Peer
	up    map[string]bool
	rr    int
}

// New 创建连接池
func New(gameID string, dis discover.Discovery) *Pool {
	return &Pool{
		gameID: gameID,
		dis:    dis,
		conns:  make(map[string]cellnet.Peer),
		up:     make(map[string]bool),
	}
}

// SetForwarder 注入下行投递回调，须在 Start 之前调用
func (g *Pool) SetForwarder(f Forwarder) {
	g.mu.Lock()
	g.fwd = f
	g.mu.Unlock()
}

// Start 注册对内处理函数并订阅 guild 实例发现（首次回调即建 connector）
func (g *Pool) Start(ctx context.Context) {
	registerHandlers(g)
	g.dis.Watch(ctx, g.reconcile)
}

// reconcile 按发现结果增删 guild connector
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
		log.Infof("guild discovery updated total=%d added=%v removed=%v", len(want), added, removed)
	}
}

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

	log.Infof("guild connector start addr=%s", addr)
}

func (g *Pool) disconnect(addr string) {
	g.mu.Lock()
	p, ok := g.conns[addr]
	delete(g.conns, addr)
	delete(g.up, addr)
	g.mu.Unlock()
	if !ok {
		return
	}
	if gp, ok2 := p.(cellnet.GenericPeer); ok2 {
		gp.Stop()
	}
	log.Infof("guild connector stop addr=%s", addr)
}

func (g *Pool) setUp(addr string, up bool) {
	g.mu.Lock()
	g.up[addr] = up
	g.mu.Unlock()
}

// Send 选择就绪连接发送 Transmit；无可用连接返回 false（game 回 guild unavailable）
func (g *Pool) Send(t *pb.Transmit) bool {
	g.mu.Lock()
	ses, ok := g.pickLocked()
	g.mu.Unlock()
	if !ok {
		return false
	}
	ses.Send(t)
	return true
}

// pickLocked 轮询选择一个已就绪的 guild 会话
func (g *Pool) pickLocked() (cellnet.Session, bool) {
	addrs := g.dis.Addrs()
	if len(addrs) == 0 {
		return nil, false
	}
	for i := 0; i < len(addrs); i++ {
		idx := (g.rr + i) % len(addrs)
		addr := addrs[idx]
		p, ok := g.conns[addr]
		if !ok || !g.up[addr] {
			continue
		}
		ses := p.(cellnet.TCPConnector).Session()
		if ses == nil {
			continue
		}
		g.rr = (idx + 1) % len(addrs)
		return ses, true
	}
	return nil, false
}
