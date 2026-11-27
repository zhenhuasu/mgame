package handler

import (
	"context"

	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/svcproc"
	"github.com/zhenhuasu/mgame/guild/service"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerRequests 逐个注册公会最终协议，每个协议绑定独立强类型处理函数
func registerRequests(h *Handler) {
	registerReq(h, &pb.CreateGuildReq{}, h.onCreateGuild)
	registerReq(h, &pb.JoinGuildReq{}, h.onJoinGuild)
	registerReq(h, &pb.LeaveGuildReq{}, h.onLeaveGuild)
	registerReq(h, &pb.KickGuildMemberReq{}, h.onKickMember)
	registerReq(h, &pb.TransferLeaderReq{}, h.onTransferLeader)
	registerReq(h, &pb.SetGuildNoticeReq{}, h.onSetNotice)
	registerReq(h, &pb.SetJoinModeReq{}, h.onSetJoinMode)
	registerReq(h, &pb.SetMemberPositionReq{}, h.onSetPosition)
	registerReq(h, &pb.DisbandGuildReq{}, h.onDisbandGuild)
	registerReq(h, &pb.GetGuildInfoReq{}, h.onGetGuildInfo)
	registerReq(h, &pb.ListGuildsReq{}, h.onListGuilds)
	registerReq(h, &pb.InviteMemberReq{}, h.onInviteMember)
	registerReq(h, &pb.ListInvitesReq{}, h.onListInvites)
	registerReq(h, &pb.RespondInviteReq{}, h.onRespondInvite)
}

// registerReq 将最终协议注册到强类型处理函数（类型由 proto 与 fn 共同推导）
func registerReq[T any](h *Handler, proto *T,
	fn func(cellnet.Event, svcproc.TransmitContext, *T)) {
	h.handle(proto, func(ev cellnet.Event, rc svcproc.TransmitContext) {
		fn(ev, rc, ev.Message().(*T))
	})
}

// reqContext 构造公会请求超时上下文与角色上下文
func reqContext(rc svcproc.TransmitContext) (context.Context, context.CancelFunc, service.RoleCtx) {
	ctx, cancel := context.WithTimeout(context.Background(), reqTimeout)
	return ctx, cancel, service.RoleCtx{RoleID: rc.RoleID, Name: rc.RoleName, Level: rc.RoleLevel}
}

// handle 注册已握手 game 的请求处理函数：队列内校验握手与 Transmit 上下文
func (h *Handler) handle(msg interface{}, fn func(cellnet.Event, svcproc.TransmitContext)) {
	net.Register(msg, func(ev cellnet.Event) {
		rc, ok := svcproc.ContextOf(ev)
		if !ok {
			log.Warnf("drop %T: missing transmit context session=%d", ev.Message(), ev.Session().ID())
			return
		}
		ses := ev.Session()
		if !h.authed[ses.ID()] {
			log.Warnf("drop %T from unauthenticated session=%d uid=%d", ev.Message(), ses.ID(), rc.UID)
			return
		}
		fn(ev, rc)
	})
}
