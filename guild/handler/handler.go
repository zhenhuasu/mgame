// Package handler guild 服务消息处理：game 握手、公会请求分发、GuildS2C 回包与推送广播。
package handler

import (
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/auth"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/guild/model"
	"github.com/zhenhuasu/mgame/guild/service"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("guild.handler")

const reqTimeout = 3 * time.Second

// Handler 消息处理单元（事件单队列串行，内部 map 无需加锁）
type Handler struct {
	mgr    *service.GuildManager
	secret string // game↔guild 共享密钥，空 = 不校验

	authed   map[int64]bool
	sessions map[int64]cellnet.Session // 已握手 game 会话（推送广播用）
}

// New 创建处理单元
func New(mgr *service.GuildManager, guildSecret string) *Handler {
	return &Handler{
		mgr:      mgr,
		secret:   guildSecret,
		authed:   make(map[int64]bool),
		sessions: make(map[int64]cellnet.Session),
	}
}

// Register 注册全部对内处理函数，须在事件循环起动前调用
func (h *Handler) Register() {
	net.Register("SessionAccepted", h.onAccepted)
	net.Register("SessionClosed", h.onClosed)
	net.Register(&pb.RegisterGame{}, h.onRegisterGame)
	registerRequests(h)
}

func (h *Handler) onAccepted(ev cellnet.Event) {
	log.Infof("game connected id=%d", ev.Session().ID())
}

func (h *Handler) onClosed(ev cellnet.Event) {
	msg := ev.Message().(*cellnet.SessionClosed)
	sid := ev.Session().ID()
	delete(h.authed, sid)
	delete(h.sessions, sid)
	log.Infof("game disconnected id=%d reason=%v", sid, msg.Reason)
}

// onRegisterGame game 注册：校验 HMAC 后才认可该会话
func (h *Handler) onRegisterGame(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterGame)
	ses := ev.Session()

	if err := auth.VerifyPeer(h.secret, msg.GameId, msg.Ts, msg.Nonce, msg.Hmac, time.Now()); err != nil {
		log.Warnf("register game rejected id=%q session=%d: %v", msg.GameId, ses.ID(), err)
		ses.Send(&pb.RegisterAck{Code: 1})
		ses.Close()
		return
	}

	h.authed[ses.ID()] = true
	h.sessions[ses.ID()] = ses
	log.Infof("register game id=%q session=%d", msg.GameId, ses.ID())
	ses.Send(&pb.RegisterAck{Code: 0})
}

// reply 编码回包并经 GuildS2C 返回给来源 game
func (h *Handler) reply(ses cellnet.Session, uid int64, msg interface{}) {
	data, meta, err := codec.EncodeMessage(msg, nil)
	if err != nil {
		log.Errorf("encode guild reply uid=%d %T: %v", uid, msg, err)
		return
	}
	ses.Send(&pb.GuildS2C{Uid: uid, MsgId: uint32(meta.ID), Payload: data})
}

// Invite 实现 service.Notifier：邀请到达推送
func (h *Handler) Invite(roleID int64, guildID, invitedBy int64, guildName, invitedByName string) {
	h.notify(roleID, &pb.GuildInviteNtf{
		GuildId:       guildID,
		GuildName:     guildName,
		InvitedBy:     invitedBy,
		InvitedByName: invitedByName,
	})
}

// Event 实现 service.Notifier：关键事件推送
func (h *Handler) Event(roleID int64, kind int32, guildID int64, guildName string, position int32, operatorRoleID int64) {
	h.notify(roleID, &pb.GuildEventNtf{
		Kind:           pb.GuildEventKind(kind),
		GuildId:        guildID,
		GuildName:      guildName,
		Position:       pb.GuildPosition(position),
		OperatorRoleId: operatorRoleID,
	})
}

// notify 编码为 GuildNotify 广播给全部已握手 game（由 game 按在线角色命中转发）
func (h *Handler) notify(roleID int64, msg interface{}) {
	data, meta, err := codec.EncodeMessage(msg, nil)
	if err != nil {
		log.Errorf("encode guild notify role=%d %T: %v", roleID, msg, err)
		return
	}
	for _, ses := range h.sessions {
		ses.Send(&pb.GuildNotify{RoleId: roleID, MsgId: uint32(meta.ID), Payload: data})
	}
}

func codeOf(err error) int32 {
	if err != nil {
		return 1
	}
	return 0
}

func reasonOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func toGuildInfo(g *model.Guild) *pb.GuildInfo {
	if g == nil {
		return nil
	}
	return &pb.GuildInfo{
		GuildId:      g.ID,
		Name:         g.Name,
		LeaderRoleId: g.LeaderRoleID,
		Notice:       g.Notice,
		Level:        g.Level,
		MemberCount:  g.MemberCount,
		JoinMode:     pb.GuildJoinMode(g.JoinMode),
	}
}

func toMemberInfo(m model.GuildMember) *pb.GuildMemberInfo {
	return &pb.GuildMemberInfo{
		RoleId:   m.RoleID,
		Name:     m.RoleName,
		Level:    m.RoleLevel,
		Position: pb.GuildPosition(m.Position),
		JoinedAt: m.JoinedAt.Unix(),
	}
}
