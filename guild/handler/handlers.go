package handler

import (
	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/svcproc"

	pb "github.com/zhenhuasu/mgame/proto"
)

// onCreateGuild 创建公会（会长）
func (h *Handler) onCreateGuild(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.CreateGuildReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	g, err := h.mgr.Create(ctx, role, r.Name)
	h.reply(ev.Session(), rc.UID, &pb.CreateGuildAck{Code: codeOf(err), Reason: reasonOf(err), Guild: toGuildInfo(g)})
}

// onJoinGuild 直接加入公会（受加入方式限制）
func (h *Handler) onJoinGuild(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.JoinGuildReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	g, err := h.mgr.Join(ctx, role, r.GuildId)
	h.reply(ev.Session(), rc.UID, &pb.JoinGuildAck{Code: codeOf(err), Reason: reasonOf(err), Guild: toGuildInfo(g)})
}

// onLeaveGuild 退出公会（会长不可退出）
func (h *Handler) onLeaveGuild(ev cellnet.Event, rc svcproc.TransmitContext, _ *pb.LeaveGuildReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.Leave(ctx, role)
	h.reply(ev.Session(), rc.UID, &pb.LeaveGuildAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onKickMember 踢出成员
func (h *Handler) onKickMember(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.KickGuildMemberReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.Kick(ctx, role, r.RoleId)
	h.reply(ev.Session(), rc.UID, &pb.KickGuildMemberAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onTransferLeader 转让会长
func (h *Handler) onTransferLeader(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.TransferLeaderReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.Transfer(ctx, role, r.RoleId)
	h.reply(ev.Session(), rc.UID, &pb.TransferLeaderAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onSetNotice 修改公会公告
func (h *Handler) onSetNotice(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.SetGuildNoticeReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.SetNotice(ctx, role, r.Notice)
	h.reply(ev.Session(), rc.UID, &pb.SetGuildNoticeAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onSetJoinMode 修改加入方式
func (h *Handler) onSetJoinMode(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.SetJoinModeReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.SetJoinMode(ctx, role, int32(r.Mode))
	h.reply(ev.Session(), rc.UID, &pb.SetJoinModeAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onSetPosition 任免副会长/降为成员
func (h *Handler) onSetPosition(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.SetMemberPositionReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.SetPosition(ctx, role, r.RoleId, int32(r.Position))
	h.reply(ev.Session(), rc.UID, &pb.SetMemberPositionAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onDisbandGuild 解散公会
func (h *Handler) onDisbandGuild(ev cellnet.Event, rc svcproc.TransmitContext, _ *pb.DisbandGuildReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.Disband(ctx, role)
	h.reply(ev.Session(), rc.UID, &pb.DisbandGuildAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onGetGuildInfo 公会信息与成员列表
func (h *Handler) onGetGuildInfo(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.GetGuildInfoReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	g, members, err := h.mgr.Info(ctx, role.RoleID, r.GuildId)
	ack := &pb.GetGuildInfoAck{Code: codeOf(err), Reason: reasonOf(err), Guild: toGuildInfo(g)}
	for _, mb := range members {
		ack.Members = append(ack.Members, toMemberInfo(mb))
	}
	h.reply(ev.Session(), rc.UID, ack)
}

// onListGuilds 公会列表
func (h *Handler) onListGuilds(ev cellnet.Event, rc svcproc.TransmitContext, _ *pb.ListGuildsReq) {
	ctx, cancel, _ := reqContext(rc)
	defer cancel()
	gs, err := h.mgr.List(ctx)
	ack := &pb.ListGuildsAck{Code: codeOf(err), Reason: reasonOf(err)}
	for _, g := range gs {
		ack.Guilds = append(ack.Guilds, toGuildInfo(&g))
	}
	h.reply(ev.Session(), rc.UID, ack)
}

// onInviteMember 邀请加入公会
func (h *Handler) onInviteMember(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.InviteMemberReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	err := h.mgr.Invite(ctx, role, r.RoleId)
	h.reply(ev.Session(), rc.UID, &pb.InviteMemberAck{Code: codeOf(err), Reason: reasonOf(err)})
}

// onListInvites 查看我的邀请
func (h *Handler) onListInvites(ev cellnet.Event, rc svcproc.TransmitContext, _ *pb.ListInvitesReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	views, err := h.mgr.ListInvites(ctx, role.RoleID)
	ack := &pb.ListInvitesAck{Code: codeOf(err), Reason: reasonOf(err)}
	for _, v := range views {
		ack.Invites = append(ack.Invites, &pb.GuildInviteInfo{
			GuildId:       v.GuildID,
			GuildName:     v.GuildName,
			InvitedBy:     v.InvitedBy,
			InvitedByName: v.InvitedByName,
			CreatedAt:     v.CreatedAt.Unix(),
		})
	}
	h.reply(ev.Session(), rc.UID, ack)
}

// onRespondInvite 接受/拒绝邀请
func (h *Handler) onRespondInvite(ev cellnet.Event, rc svcproc.TransmitContext, r *pb.RespondInviteReq) {
	ctx, cancel, role := reqContext(rc)
	defer cancel()
	g, err := h.mgr.RespondInvite(ctx, role, r.GuildId, r.Accept)
	h.reply(ev.Session(), rc.UID, &pb.RespondInviteAck{Code: codeOf(err), Reason: reasonOf(err), Guild: toGuildInfo(g)})
}
