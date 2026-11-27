package handler

import (
	"context"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/core/svcproc"
	"github.com/zhenhuasu/mgame/game/model"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerGuildHandlers 逐个注册公会最终协议，失败 Ack 与协议在注册处绑定
func registerGuildHandlers(h *Handler) {
	registerGuildReq(h, &pb.CreateGuildReq{}, func(s string) interface{} {
		return &pb.CreateGuildAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.JoinGuildReq{}, func(s string) interface{} {
		return &pb.JoinGuildAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.LeaveGuildReq{}, func(s string) interface{} {
		return &pb.LeaveGuildAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.KickGuildMemberReq{}, func(s string) interface{} {
		return &pb.KickGuildMemberAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.TransferLeaderReq{}, func(s string) interface{} {
		return &pb.TransferLeaderAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.SetGuildNoticeReq{}, func(s string) interface{} {
		return &pb.SetGuildNoticeAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.SetJoinModeReq{}, func(s string) interface{} {
		return &pb.SetJoinModeAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.SetMemberPositionReq{}, func(s string) interface{} {
		return &pb.SetMemberPositionAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.DisbandGuildReq{}, func(s string) interface{} {
		return &pb.DisbandGuildAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.GetGuildInfoReq{}, func(s string) interface{} {
		return &pb.GetGuildInfoAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.ListGuildsReq{}, func(s string) interface{} {
		return &pb.ListGuildsAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.ListInvitesReq{}, func(s string) interface{} {
		return &pb.ListInvitesAck{Code: 1, Reason: s}
	})
	registerGuildReq(h, &pb.RespondInviteReq{}, func(s string) interface{} {
		return &pb.RespondInviteAck{Code: 1, Reason: s}
	})
	// 邀请需先按角色名解析 role_id，单独注册强类型处理函数
	h.registerAuthed(&pb.InviteMemberReq{}, h.onInviteMember)
}

// registerGuildReq 注册公会转发协议与对应失败 Ack 构造器
func registerGuildReq(h *Handler, proto interface{}, errAck func(string) interface{}) {
	h.registerAuthed(proto, func(ev cellnet.Event, rc svcproc.TransmitContext) {
		h.forwardGuild(ev, rc, errAck)
	})
}

// forwardGuild 通用转发：校验在线角色后编码并发送至 guild
func (h *Handler) forwardGuild(ev cellnet.Event, rc svcproc.TransmitContext, errAck func(string) interface{}) {
	role, ok := h.requireRole(ev.Session(), rc, errAck)
	if !ok {
		return
	}
	h.sendGuild(ev.Session(), rc, role, ev.Message(), errAck)
}

// onInviteMember 邀请目标按角色名（全局唯一）解析 role_id 后转发
func (h *Handler) onInviteMember(ev cellnet.Event, rc svcproc.TransmitContext) {
	inv := ev.Message().(*pb.InviteMemberReq)
	ses := ev.Session()

	role, ok := h.requireRole(ses, rc, errAckInviteMember)
	if !ok {
		return
	}
	if inv.RoleId == 0 {
		if inv.Name == "" {
			h.replyForwardS2C(ses, rc.UID, errAckInviteMember("role not found"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		target, found, err := h.mgr.RoleByName(ctx, inv.Name)
		cancel()
		if err != nil {
			log.Warnf("guild invite resolve name=%q: %v", inv.Name, err)
			h.replyForwardS2C(ses, rc.UID, errAckInviteMember(err.Error()))
			return
		}
		if !found {
			h.replyForwardS2C(ses, rc.UID, errAckInviteMember("role not found"))
			return
		}
		inv.RoleId = target.ID
	}
	h.sendGuild(ses, rc, role, inv, errAckInviteMember)
}

// requireRole 取在线角色；未进入游戏时用对应协议 Ack 本地回错
func (h *Handler) requireRole(ses cellnet.Session, rc svcproc.TransmitContext,
	errAck func(string) interface{}) (*model.Role, bool) {
	role, ok := h.mgr.OnlineRole(rc.UID)
	if !ok {
		h.replyForwardS2C(ses, rc.UID, errAck("not entered game"))
	}
	return role, ok
}

// sendGuild 编码请求并经 Transmit 发送；无可用 guild 时本地回错
func (h *Handler) sendGuild(ses cellnet.Session, rc svcproc.TransmitContext,
	role *model.Role, req interface{}, errAck func(string) interface{}) {
	data, meta, err := codec.EncodeMessage(req, nil)
	if err != nil {
		log.Errorf("encode guild req uid=%d %T: %v", rc.UID, req, err)
		h.replyForwardS2C(ses, rc.UID, errAck("encode failed"))
		return
	}

	t := &pb.Transmit{
		Uid:       rc.UID,
		RoleId:    role.ID,
		RoleName:  role.Name,
		RoleLevel: role.Level,
		MsgId:     uint32(meta.ID),
		Payload:   data,
	}
	if h.guild == nil || !h.guild.Send(t) {
		log.Warnf("guild unavailable uid=%d role=%d %T", rc.UID, role.ID, req)
		h.replyForwardS2C(ses, rc.UID, errAck("guild unavailable"))
	}
}

func errAckInviteMember(reason string) interface{} {
	return &pb.InviteMemberAck{Code: 1, Reason: reason}
}

// ForwardGuildS2C 实现 guildclient.Forwarder：公会回包经 ForwardS2C 返回客户端
func (h *Handler) ForwardGuildS2C(uid int64, msgID uint32, payload []byte) {
	ou, ok := h.mgr.Get(uid)
	if !ok || ou.Session == nil {
		log.Warnf("guild s2c drop uid=%d msgid=%d: client offline", uid, msgID)
		return
	}
	ou.Session.Send(&pb.ForwardS2C{Uid: uid, MsgId: msgID, Payload: payload})
}

// NotifyGuild 实现 guildclient.Forwarder：推送命中在线角色后转发
func (h *Handler) NotifyGuild(roleID int64, msgID uint32, payload []byte) {
	uid, ok := h.mgr.FindOnlineByRole(roleID)
	if !ok {
		return
	}
	h.ForwardGuildS2C(uid, msgID, payload)
}
