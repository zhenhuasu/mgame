package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/auth"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"

	pb "github.com/zhenhuasu/mgame/proto"
)

// games gate → game 连接池（Connect 时初始化）
var games *GamePool

// Connect 连接 game 服务（第2步）：注册对内处理函数，按发现结果建/拆 Connector。
// net.Init 创建了注册中心时用其发现 game，否则回退静态 GameServers。
func Connect(ctx context.Context, cfg *config.Gate) {
	var dis Discovery = StaticDiscovery(cfg.GameServers)
	if reg := net.Registry(); reg != nil {
		dis = NewRegistryDiscovery(reg, registry.ServiceGame, dis)
	}
	games = NewGamePool(cfg.GateID, dis)
	games.start(ctx)
}

type gameRoute struct {
	addr    string
	account string
}

// GamePool gate → game 连接池：每个 game 地址一个 tcp.Connector（自动重连），
// RoundRobin 分配登录玩家，维护 uid → game 路由。
// 事件回调与客户端消息同在一个 EventQueue 内串行执行；加锁仅为防御未来跨 goroutine 调用。
type GamePool struct {
	gateID string
	dis    Discovery

	mu      sync.Mutex
	conns   map[string]cellnet.Peer // gameAddr → tcp.Connector
	rr      int
	routes  map[int64]*gameRoute // uid → 路由
	account map[int64]string     // uid → account（重连补发 PlayerOnline 用）
}

func NewGamePool(gateID string, dis Discovery) *GamePool {
	return &GamePool{
		gateID:  gateID,
		dis:     dis,
		conns:   make(map[string]cellnet.Peer),
		routes:  make(map[int64]*gameRoute),
		account: make(map[int64]string),
	}
}

// start 注册对内处理函数，订阅实例发现（首次回调即建 connector）。
// 事件在 net.Run 起循环后才派发；register 须在其之前完成。
func (g *GamePool) start(ctx context.Context) {
	net.Register("SessionConnected", g.onConnected)
	net.Register("SessionClosed", g.onClosed)
	net.Register(&pb.RegisterAck{}, g.onRegisterAck)
	net.Register(&pb.ForwardS2C{}, g.onForwardS2C)

	g.dis.Watch(ctx, g.reconcile)
}

// reconcile 按发现结果增删 game connector。
// 回调来自单一 watch goroutine（静态实现为 start 同步回调一次），无并发 reconcile。
func (g *GamePool) reconcile(addrs []string) {
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
func (g *GamePool) connect(addr string) {
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
func (g *GamePool) disconnect(addr string) {
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

// onConnected game 连接建立/重连。peer 名即 game 地址。
func (g *GamePool) onConnected(ev cellnet.Event) {
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	log.Infof("game connected addr=%s", addr)
	ev.Session().Send(signRegisterGate(g.gateID))
	// 断线重连后 game 状态已丢，补发路由中玩家的在线状态
	for _, uid := range g.uidsOn(addr) {
		ev.Session().Send(&pb.PlayerOnline{Uid: uid, Account: g.accountOf(uid)})
		log.Infof("re-register player uid=%d addr=%s", uid, addr)
	}
}

// onClosed game 连接断开。SessionClosed 为全局 key（ws 客户端断开也命中），
// 按 peer 类型分流，只处理 game Connector。
func (g *GamePool) onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	log.Warnf("game disconnected addr=%s reason=%v", addr, msg.Reason)
}

// onRegisterAck game 确认注册
func (g *GamePool) onRegisterAck(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterAck)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	log.Infof("game registered addr=%s code=%d", addr, msg.Code)
}

// onForwardS2C game → 客户端业务回包，按 uid 投递
func (g *GamePool) onForwardS2C(ev cellnet.Event) {
	msg := ev.Message().(*pb.ForwardS2C)
	ses := mgr.SessionByUID(msg.Uid)
	if ses == nil {
		log.Warnf("forward s2c drop uid=%d: client offline", msg.Uid)
		return
	}
	ses.Send(&cellnet.RawPacket{MsgID: int(msg.MsgId), MsgData: msg.Payload})
}

// Assign 为 uid 轮询分配 game。返回 (新地址, 旧地址, 是否成功)；
// 旧地址非空且与新地址不同时，调用方需向旧实例发 PlayerOffline(reason=2)。
func (g *GamePool) Assign(uid int64, account string) (string, string, bool) {
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
func (g *GamePool) Unassign(uid int64) (string, bool) {
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
func (g *GamePool) RouteOf(uid int64) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	r, ok := g.routes[uid]
	if !ok {
		return "", false
	}
	return r.addr, true
}

// SendTo 向指定 game 地址发消息；未连接返回 false
func (g *GamePool) SendTo(addr string, msg interface{}) bool {
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

// EncodeForward 将业务消息编码为 ForwardC2S 信封
func EncodeForward(uid int64, msg interface{}) (*pb.ForwardC2S, error) {
	data, meta, err := codec.EncodeMessage(msg, nil)
	if err != nil {
		return nil, err
	}
	return &pb.ForwardC2S{Uid: uid, MsgId: uint32(meta.ID), Payload: data}, nil
}

// pickLocked 轮询选择一个已连接的 game
func (g *GamePool) pickLocked() (string, bool) {
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
func (g *GamePool) uidsOn(addr string) []int64 {
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

func (g *GamePool) accountOf(uid int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.account[uid]
}

// signRegisterGate 构造带 HMAC 的握手消息；gateSecret 为空则留空（联调）
func signRegisterGate(gateID string) *pb.RegisterGate {
	req := &pb.RegisterGate{GateId: gateID}
	if gateSecret == "" {
		return req
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return req
	}
	req.Ts = time.Now().Unix()
	req.Nonce = hex.EncodeToString(b[:])
	req.Hmac = auth.SignGate(gateSecret, gateID, req.Ts, req.Nonce)
	return req
}
