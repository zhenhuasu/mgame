package handler

import (
	"context"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/core/svcproc"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerForwardHandlers 注册客户端最终协议处理函数（svcproc 已解包 Transmit）
func registerForwardHandlers(h *Handler) {
	h.registerAuthed(&pb.HelloReq{}, h.onHello)
	h.registerAuthed(&pb.CreateRoleReq{}, h.onCreateRole)
	h.registerAuthed(&pb.EnterGameReq{}, h.onEnterGame)
	registerGuildHandlers(h)
}

// onHello 演示消息
func (h *Handler) onHello(ev cellnet.Event, rc svcproc.TransmitContext) {
	req := ev.Message().(*pb.HelloReq)
	if err := sendForwardS2C(ev.Session(), rc.UID, &pb.HelloAck{Content: req.Content}); err != nil {
		log.Errorf("encode helloack uid=%d: %v", rc.UID, err)
		return
	}
	log.Infof("hello uid=%d content=%q", rc.UID, req.Content)
}

// onCreateRole 创建角色（须已登录进本实例）
func (h *Handler) onCreateRole(ev cellnet.Event, rc svcproc.TransmitContext) {
	req := ev.Message().(*pb.CreateRoleReq)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	role, err := h.mgr.CreateRole(ctx, rc.UID, req.Name)
	if err != nil {
		log.Warnf("create role uid=%d name=%q: %v", rc.UID, req.Name, err)
		h.replyForwardS2C(ev.Session(), rc.UID, &pb.CreateRoleAck{Code: 1, Reason: err.Error()})
		return
	}
	log.Infof("create role uid=%d account-role=%d name=%q", rc.UID, role.ID, role.Name)
	h.replyForwardS2C(ev.Session(), rc.UID, &pb.CreateRoleAck{Code: 0, Role: toRoleInfo1(role)})
}

// onEnterGame 进入游戏：选择/自动创建角色并置为在线角色
func (h *Handler) onEnterGame(ev cellnet.Event, rc svcproc.TransmitContext) {
	req := ev.Message().(*pb.EnterGameReq)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	role, err := h.mgr.EnterGame(ctx, rc.UID, req.RoleId)
	if err != nil {
		log.Warnf("enter game uid=%d role_id=%d: %v", rc.UID, req.RoleId, err)
		h.replyForwardS2C(ev.Session(), rc.UID, &pb.EnterGameAck{Code: 1, Reason: err.Error()})
		return
	}
	log.Infof("enter game uid=%d role_id=%d name=%q", rc.UID, role.ID, role.Name)
	h.replyForwardS2C(ev.Session(), rc.UID, &pb.EnterGameAck{Code: 0, Role: toRoleInfo1(role)})
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
