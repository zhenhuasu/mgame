package handler

import (
	"time"

	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/auth"

	pb "github.com/zhenhuasu/mgame/proto"
)

// onAccepted gate 连入
func (h *Handler) onAccepted(ev cellnet.Event) {
	log.Infof("gate connected id=%d", ev.Session().ID())
}

// onClosed gate 断开：摘除其名下在线记录（不清在线角色）与握手状态
func (h *Handler) onClosed(ev cellnet.Event) {
	msg := ev.Message().(*cellnet.SessionClosed)

	removed := h.mgr.RemoveBySession(ev.Session().ID())
	delete(h.authed, ev.Session().ID())
	log.Infof("gate disconnected id=%d reason=%v removed=%v", ev.Session().ID(), msg.Reason, removed)
}

// onRegisterGate gate 注册：校验 HMAC 后才认可该会话
func (h *Handler) onRegisterGate(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterGate)
	ses := ev.Session()

	if err := auth.VerifyGate(h.secret, msg.GateId, msg.Ts, msg.Nonce, msg.Hmac, time.Now()); err != nil {
		log.Warnf("register gate rejected id=%s session=%d: %v", msg.GateId, ses.ID(), err)
		ses.Send(&pb.RegisterAck{Code: 1})
		ses.Close()
		return
	}

	h.authed[ses.ID()] = true
	log.Infof("register gate id=%s session=%d", msg.GateId, ses.ID())
	ses.Send(&pb.RegisterAck{Code: 0})
}
