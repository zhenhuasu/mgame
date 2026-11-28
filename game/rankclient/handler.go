package rankclient

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

func encode(msg interface{}) ([]byte, *cellnet.MessageMeta, error) {
	return codec.EncodeMessage(msg, nil)
}

func registerHandlers(g *Pool) {
	net.Register("SessionConnected", g.onConnected)
	net.Register("SessionClosed", g.onClosed)
	net.Register(&pb.RegisterRankAck{}, g.onRegisterAck)
	net.Register(&pb.RankChangeNotify{}, g.onRankChange)
}

func (g *Pool) onConnected(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	if !g.owns(addr) {
		return
	}
	log.Infof("rank connected addr=%s", addr)
	ev.Session().Send(signRegisterRank(g.gameID))
}

func (g *Pool) onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "tcp.Connector" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	if !g.owns(addr) {
		return
	}
	g.setUp(addr, false)
	log.Warnf("rank disconnected addr=%s reason=%v", addr, msg.Reason)
}

func (g *Pool) onRegisterAck(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterRankAck)
	addr := ev.Session().Peer().(cellnet.PeerProperty).Name()
	if msg.Code != 0 {
		log.Warnf("rank register rejected addr=%s code=%d", addr, msg.Code)
		return
	}
	g.setUp(addr, true)
	log.Infof("rank registered addr=%s", addr)
}

func (g *Pool) onRankChange(ev cellnet.Event) {
	msg := ev.Message().(*pb.RankChangeNotify)
	g.mu.Lock()
	fwd := g.fwd
	g.mu.Unlock()
	if fwd == nil {
		return
	}
	fwd.NotifyRankChange(msg.RoleId, msg.Board, msg.OldRank, msg.NewRank, msg.Score)
}
