package main

import (
	"fmt"
	"time"

	"github.com/davyxu/cellnet"

	pb "github.com/zhenhuasu/mgame/proto"
)

// guildOpts 公会操作参数
type guildOpts struct {
	op         string
	name       string
	id         int64
	target     int64
	targetName string
	notice     string
	joinMode   string
	position   string
	accept     bool
}

// runGuild 发送一个公会请求并打印回包；失败退出
func runGuild(conn cellnet.Session, o guildOpts, waitFor func(string, time.Duration) (interface{}, bool), exit func(int)) {
	var req interface{}
	var kind string

	switch o.op {
	case "create":
		if o.name == "" {
			log.Errorf("create requires -guild-name")
			exit(1)
		}
		req, kind = &pb.CreateGuildReq{Name: o.name}, "createguildack"

	case "join":
		req, kind = &pb.JoinGuildReq{GuildId: o.id}, "joinguildack"

	case "leave":
		req, kind = &pb.LeaveGuildReq{}, "leaveguildack"

	case "kick":
		req, kind = &pb.KickGuildMemberReq{RoleId: o.target}, "kickguildack"

	case "transfer":
		req, kind = &pb.TransferLeaderReq{RoleId: o.target}, "transferguildack"

	case "notice":
		req, kind = &pb.SetGuildNoticeReq{Notice: o.notice}, "noticeguildack"

	case "set-join-mode":
		mode, ok := parseJoinMode(o.joinMode)
		if !ok {
			log.Errorf("bad -join-mode %q (open|invite_only)", o.joinMode)
			exit(1)
		}
		req, kind = &pb.SetJoinModeReq{Mode: mode}, "joinmodeguildack"

	case "set-position":
		pos, ok := parsePosition(o.position)
		if !ok {
			log.Errorf("bad -position %q (vice|member)", o.position)
			exit(1)
		}
		req, kind = &pb.SetMemberPositionReq{RoleId: o.target, Position: pos}, "positionguildack"

	case "disband":
		req, kind = &pb.DisbandGuildReq{}, "disbandguildack"

	case "info":
		req, kind = &pb.GetGuildInfoReq{GuildId: o.id}, "infoguildack"

	case "list":
		req, kind = &pb.ListGuildsReq{}, "listguildsack"

	case "invite":
		req, kind = &pb.InviteMemberReq{RoleId: o.target, Name: o.targetName}, "inviteguildack"

	case "invites":
		req, kind = &pb.ListInvitesReq{}, "invitesguildack"

	case "respond":
		req, kind = &pb.RespondInviteReq{GuildId: o.id, Accept: o.accept}, "respondguildack"

	default:
		log.Errorf("unknown -guild %q", o.op)
		exit(1)
	}

	conn.Send(req)
	raw, ok := waitFor(kind, 3*time.Second)
	if !ok {
		log.Errorf("%s ack timeout", o.op)
		exit(1)
	}
	code, reason, desc := guildAckResult(raw)
	if code != 0 {
		log.Errorf("%s failed code=%d reason=%q", o.op, code, reason)
		exit(1)
	}
	if desc != "" {
		log.Infof("%s ok:\n%s", o.op, desc)
	} else {
		log.Infof("%s ok", o.op)
	}
}

func parseJoinMode(s string) (pb.GuildJoinMode, bool) {
	switch s {
	case "open":
		return pb.GuildJoinMode_GUILD_JOIN_OPEN, true
	case "invite_only", "invite":
		return pb.GuildJoinMode_GUILD_JOIN_INVITE_ONLY, true
	}
	return 0, false
}

func parsePosition(s string) (pb.GuildPosition, bool) {
	switch s {
	case "vice", "admin":
		return pb.GuildPosition_GUILD_POSITION_VICE, true
	case "member":
		return pb.GuildPosition_GUILD_POSITION_MEMBER, true
	}
	return 0, false
}

// guildAckResult 归一化各类公会 Ack 的 code/reason 与可读描述
func guildAckResult(ack interface{}) (int32, string, string) {
	switch a := ack.(type) {
	case *pb.CreateGuildAck:
		return a.Code, a.Reason, guildText(a.Guild)
	case *pb.JoinGuildAck:
		return a.Code, a.Reason, guildText(a.Guild)
	case *pb.LeaveGuildAck:
		return a.Code, a.Reason, ""
	case *pb.KickGuildMemberAck:
		return a.Code, a.Reason, ""
	case *pb.TransferLeaderAck:
		return a.Code, a.Reason, ""
	case *pb.SetGuildNoticeAck:
		return a.Code, a.Reason, ""
	case *pb.SetJoinModeAck:
		return a.Code, a.Reason, ""
	case *pb.SetMemberPositionAck:
		return a.Code, a.Reason, ""
	case *pb.DisbandGuildAck:
		return a.Code, a.Reason, ""
	case *pb.GetGuildInfoAck:
		desc := guildText(a.Guild)
		for _, m := range a.Members {
			desc += fmt.Sprintf("\n  member role=%d name=%q level=%d position=%d", m.RoleId, m.Name, m.Level, m.Position)
		}
		return a.Code, a.Reason, desc
	case *pb.ListGuildsAck:
		desc := fmt.Sprintf("  %d guild(s)", len(a.Guilds))
		for _, g := range a.Guilds {
			desc += "\n  " + guildText(g)
		}
		return a.Code, a.Reason, desc
	case *pb.InviteMemberAck:
		return a.Code, a.Reason, ""
	case *pb.ListInvitesAck:
		desc := fmt.Sprintf("  %d invite(s)", len(a.Invites))
		for _, inv := range a.Invites {
			desc += fmt.Sprintf("\n  guild=%d %q by=%d(%q)", inv.GuildId, inv.GuildName, inv.InvitedBy, inv.InvitedByName)
		}
		return a.Code, a.Reason, desc
	case *pb.RespondInviteAck:
		return a.Code, a.Reason, guildText(a.Guild)
	}
	return 0, "", ""
}

func guildText(g *pb.GuildInfo) string {
	if g == nil {
		return ""
	}
	return fmt.Sprintf("guild id=%d name=%q leader=%d members=%d level=%d join_mode=%d notice=%q",
		g.GuildId, g.Name, g.LeaderRoleId, g.MemberCount, g.Level, g.JoinMode, g.Notice)
}
