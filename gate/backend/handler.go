package backend

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("gate.backend")

// registerPoolHandlers 注册对内（game 连接侧）处理函数，须在起循环前调用
func registerPoolHandlers(g *Pool) {
	net.Register("SessionConnected", g.onConnected)
	net.Register("SessionClosed", g.onClosed)
	net.Register(&pb.RegisterAck{}, g.onRegisterAck)
	net.Register(&pb.PlayerLoginAck{}, g.onPlayerLoginAck)
	net.Register(&pb.ForwardS2C{}, g.onForwardS2C)
}

// onConnected game 连接建立/重连。peer 名即 game 地址。
func (g *Pool) onConnected(ev cellnet.Event) {
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	g.setUp(addr, true)
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
func (g *Pool) onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	g.setUp(addr, false)
	log.Warnf("game disconnected addr=%s reason=%v", addr, msg.Reason)
}

// onRegisterAck game 确认注册
func (g *Pool) onRegisterAck(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterAck)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	log.Infof("game registered addr=%s code=%d", addr, msg.Code)
}

// onPlayerLoginAck game 回登录结果 → 投递 LoginAck（含角色列表）给客户端
func (g *Pool) onPlayerLoginAck(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerLoginAck)
	ses := g.hub.SessionByUID(msg.Uid)
	if ses == nil {
		// 客户端已下线：路由已在 onClosed 清理，仅补日志
		log.Warnf("login ack drop uid=%d: client offline code=%d", msg.Uid, msg.Code)
		return
	}
	if msg.Code != 0 {
		// 登录失败：回滚路由与登录态，避免半开
		if addr, ok := g.Unassign(msg.Uid); ok {
			g.SendTo(addr, &pb.PlayerOffline{Uid: msg.Uid, Reason: 1})
		}
		g.hub.RemoveBySession(ses.ID())
	}
	log.Infof("login ack uid=%d code=%d reason=%q roles=%d", msg.Uid, msg.Code, msg.Reason, len(msg.Roles))
	ses.Send(&pb.LoginAck{Code: msg.Code, Reason: msg.Reason, Uid: msg.Uid, Roles: msg.Roles})
}

// onForwardS2C game → 客户端业务回包，按 uid 投递
func (g *Pool) onForwardS2C(ev cellnet.Event) {
	msg := ev.Message().(*pb.ForwardS2C)
	ses := g.hub.SessionByUID(msg.Uid)
	if ses == nil {
		log.Warnf("forward s2c drop uid=%d: client offline", msg.Uid)
		return
	}
	ses.Send(&cellnet.RawPacket{MsgID: int(msg.MsgId), MsgData: msg.Payload})
}
