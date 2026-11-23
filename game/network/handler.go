package network

import (
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/core/auth"
	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

// player 在线玩家 → 所属 gate 连接会话
var players = make(map[int64]cellnet.Session)

// authed 已完成 RegisterGate 握手的 gate 会话（sessionID）
var authed = make(map[int64]bool)

// gateSecret gate↔game 共享密钥（Listen 时从配置注入；空 = 不校验）
var gateSecret string

func isAuthed(ses cellnet.Session) bool {
	return authed[ses.ID()]
}

// registerHandlers 注册对内处理函数。须在 net.Run 起循环前调用。
func registerHandlers() {
	net.Register("SessionAccepted", onAccepted)
	net.Register("SessionClosed", onClosed)
	net.Register(&pb.RegisterGate{}, onRegisterGate)
	net.Register(&pb.PlayerOnline{}, onPlayerOnline)
	net.Register(&pb.PlayerOffline{}, onPlayerOffline)
	net.Register(&pb.ForwardC2S{}, onForwardC2S)
}

// onAccepted gate 连入
func onAccepted(ev cellnet.Event) {
	log.Infof("gate connected id=%d", ev.Session().ID())
}

// onClosed gate 断开：清理其名下玩家
func onClosed(ev cellnet.Event) {
	msg := ev.Message().(*cellnet.SessionClosed)

	var removed []int64
	for uid, ses := range players {
		if ses.ID() == ev.Session().ID() {
			removed = append(removed, uid)
		}
	}
	for _, uid := range removed {
		delete(players, uid)
	}
	delete(authed, ev.Session().ID())
	log.Infof("gate disconnected id=%d reason=%v removed=%v", ev.Session().ID(), msg.Reason, removed)
}

// onRegisterGate gate 注册：校验 HMAC 后才认可该会话
func onRegisterGate(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterGate)
	ses := ev.Session()

	if err := auth.VerifyGate(gateSecret, msg.GateId, msg.Ts, msg.Nonce, msg.Hmac, time.Now()); err != nil {
		log.Warnf("register gate rejected id=%s session=%d: %v", msg.GateId, ses.ID(), err)
		ses.Send(&pb.RegisterAck{Code: 1})
		ses.Close()
		return
	}

	authed[ses.ID()] = true
	log.Infof("register gate id=%s session=%d", msg.GateId, ses.ID())
	ses.Send(&pb.RegisterAck{Code: 0})
}

// onPlayerOnline 玩家上线
func onPlayerOnline(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerOnline)
	if !isAuthed(ev.Session()) {
		log.Warnf("drop PlayerOnline from unauthenticated session=%d uid=%d", ev.Session().ID(), msg.Uid)
		return
	}
	players[msg.Uid] = ev.Session()
	log.Infof("player online uid=%d account=%s", msg.Uid, msg.Account)
}

// onPlayerOffline 玩家下线
func onPlayerOffline(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerOffline)
	if !isAuthed(ev.Session()) {
		log.Warnf("drop PlayerOffline from unauthenticated session=%d uid=%d", ev.Session().ID(), msg.Uid)
		return
	}
	if ses, ok := players[msg.Uid]; ok && ses.ID() == ev.Session().ID() {
		delete(players, msg.Uid)
		log.Infof("player offline uid=%d reason=%d", msg.Uid, msg.Reason)
	}
}

// onForwardC2S gate 转发的客户端业务消息
func onForwardC2S(ev cellnet.Event) {
	if !isAuthed(ev.Session()) {
		fwd := ev.Message().(*pb.ForwardC2S)
		log.Warnf("drop ForwardC2S from unauthenticated session=%d uid=%d", ev.Session().ID(), fwd.Uid)
		return
	}
	handleForwardC2S(ev.Session(), ev.Message().(*pb.ForwardC2S))
}

// handleForwardC2S 按 msgID 解码业务消息并处理，回包经 ForwardS2C 信封返回
func handleForwardC2S(gateSes cellnet.Session, fwd *pb.ForwardC2S) {
	msg, _, err := codec.DecodeMessage(int(fwd.MsgId), fwd.Payload)
	if err != nil {
		log.Errorf("decode forward uid=%d msgid=%d: %v", fwd.Uid, fwd.MsgId, err)
		return
	}

	switch bmsg := msg.(type) {
	case *pb.HelloReq:
		data, meta, err := codec.EncodeMessage(&pb.HelloAck{Content: bmsg.Content}, nil)
		if err != nil {
			log.Errorf("encode helloack: %v", err)
			return
		}
		gateSes.Send(&pb.ForwardS2C{
			Uid:     fwd.Uid,
			MsgId:   uint32(meta.ID),
			Payload: data,
		})
		log.Infof("hello uid=%d content=%q", fwd.Uid, bmsg.Content)

	default:
		log.Debugf("unhandled forward uid=%d %T", fwd.Uid, msg)
	}
}
