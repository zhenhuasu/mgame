package guildclient

import (
	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerHandlers 注册 guild 连接侧处理函数，须在起循环前调用
func registerHandlers(g *Pool) {
	net.Register("SessionConnected", g.onConnected)
	net.Register("SessionClosed", g.onClosed)
	net.Register(&pb.RegisterAck{}, g.onRegisterAck)
	net.Register(&pb.GuildS2C{}, g.onGuildS2C)
	net.Register(&pb.GuildNotify{}, g.onGuildNotify)
}

// onConnected guild 连接建立/重连。peer 名即 guild 地址。
func (g *Pool) onConnected(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	g.setUp(addr, true)
	log.Infof("guild connected addr=%s", addr)
	ev.Session().Send(signRegisterGame(g.gameID))
}

// onClosed guild 连接断开（只处理 Connector，gate 连接由 game handler 处理）
func (g *Pool) onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	g.setUp(addr, false)
	log.Warnf("guild disconnected addr=%s reason=%v", addr, msg.Reason)
}

// onRegisterAck guild 确认注册
func (g *Pool) onRegisterAck(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterAck)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	if msg.Code != 0 {
		log.Warnf("guild register rejected addr=%s code=%d", addr, msg.Code)
		return
	}
	log.Infof("guild registered addr=%s", addr)
}

// onGuildS2C guild 回包 → 客户端（经 gate 的 ForwardS2C）
func (g *Pool) onGuildS2C(ev cellnet.Event) {
	msg := ev.Message().(*pb.GuildS2C)
	g.mu.Lock()
	fwd := g.fwd
	g.mu.Unlock()
	if fwd == nil {
		log.Warnf("guild s2c drop uid=%d msgid=%d: no forwarder", msg.Uid, msg.MsgId)
		return
	}
	fwd.ForwardGuildS2C(msg.Uid, msg.MsgId, msg.Payload)
}

// onGuildNotify guild 推送 → 命中在线角色后转发
func (g *Pool) onGuildNotify(ev cellnet.Event) {
	msg := ev.Message().(*pb.GuildNotify)
	g.mu.Lock()
	fwd := g.fwd
	g.mu.Unlock()
	if fwd == nil {
		return
	}
	fwd.NotifyGuild(msg.RoleId, msg.MsgId, msg.Payload)
}
