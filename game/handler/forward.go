package handler

import (
	"context"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerForwardHandlers 注册业务转发处理函数
func registerForwardHandlers(h *Handler) {
	net.Register(&pb.ForwardC2S{}, h.onForwardC2S)
}

// onForwardC2S gate 转发的客户端业务消息
func (h *Handler) onForwardC2S(ev cellnet.Event) {
	if !h.isAuthed(ev.Session()) {
		fwd := ev.Message().(*pb.ForwardC2S)
		log.Warnf("drop ForwardC2S from unauthenticated session=%d uid=%d", ev.Session().ID(), fwd.Uid)
		return
	}
	h.handleForwardC2S(ev.Session(), ev.Message().(*pb.ForwardC2S))
}

// handleForwardC2S 按 msgID 解码业务消息并处理，回包经 ForwardS2C 信封返回
func (h *Handler) handleForwardC2S(gateSes cellnet.Session, fwd *pb.ForwardC2S) {
	msg, _, err := codec.DecodeMessage(int(fwd.MsgId), fwd.Payload)
	if err != nil {
		log.Errorf("decode forward uid=%d msgid=%d: %v", fwd.Uid, fwd.MsgId, err)
		return
	}

	switch bmsg := msg.(type) {
	case *pb.HelloReq:
		if err := sendForwardS2C(gateSes, fwd.Uid, &pb.HelloAck{Content: bmsg.Content}); err != nil {
			log.Errorf("encode helloack uid=%d: %v", fwd.Uid, err)
			return
		}
		log.Infof("hello uid=%d content=%q", fwd.Uid, bmsg.Content)

	case *pb.CreateRoleReq:
		h.onCreateRole(gateSes, fwd, bmsg)

	case *pb.EnterGameReq:
		h.onEnterGame(gateSes, fwd, bmsg)

	default:
		log.Debugf("unhandled forward uid=%d %T", fwd.Uid, msg)
	}
}

// onCreateRole 创建角色（须已登录进本实例）
func (h *Handler) onCreateRole(gateSes cellnet.Session, fwd *pb.ForwardC2S, req *pb.CreateRoleReq) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	role, err := h.mgr.CreateRole(ctx, fwd.Uid, req.Name)
	if err != nil {
		log.Warnf("create role uid=%d name=%q: %v", fwd.Uid, req.Name, err)
		h.replyForwardS2C(gateSes, fwd.Uid, &pb.CreateRoleAck{Code: 1, Reason: err.Error()})
		return
	}
	log.Infof("create role uid=%d account-role=%d name=%q", fwd.Uid, role.ID, role.Name)
	h.replyForwardS2C(gateSes, fwd.Uid, &pb.CreateRoleAck{Code: 0, Role: toRoleInfo1(role)})
}

// onEnterGame 进入游戏：选择/自动创建角色并置为在线角色
func (h *Handler) onEnterGame(gateSes cellnet.Session, fwd *pb.ForwardC2S, req *pb.EnterGameReq) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	role, err := h.mgr.EnterGame(ctx, fwd.Uid, req.RoleId)
	if err != nil {
		log.Warnf("enter game uid=%d role_id=%d: %v", fwd.Uid, req.RoleId, err)
		h.replyForwardS2C(gateSes, fwd.Uid, &pb.EnterGameAck{Code: 1, Reason: err.Error()})
		return
	}
	log.Infof("enter game uid=%d role_id=%d name=%q", fwd.Uid, role.ID, role.Name)
	h.replyForwardS2C(gateSes, fwd.Uid, &pb.EnterGameAck{Code: 0, Role: toRoleInfo1(role)})
}

// replyForwardS2C 编码业务回包并经 ForwardS2C 返回给客户端
func (h *Handler) replyForwardS2C(gateSes cellnet.Session, uid int64, msg interface{}) {
	if err := sendForwardS2C(gateSes, uid, msg); err != nil {
		log.Errorf("encode forward reply uid=%d %T: %v", uid, msg, err)
	}
}

// sendForwardS2C 编码并发送 ForwardS2C 信封
func sendForwardS2C(gateSes cellnet.Session, uid int64, msg interface{}) error {
	data, meta, err := codec.EncodeMessage(msg, nil)
	if err != nil {
		return err
	}
	gateSes.Send(&pb.ForwardS2C{
		Uid:     uid,
		MsgId:   uint32(meta.ID),
		Payload: data,
	})
	return nil
}
