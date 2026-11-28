// Package handler game 服务消息处理：gate 握手、玩家生命周期、角色与业务转发。
// 由 network.Listen 在起事件循环前调用 Register 注册。
package handler

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/svcproc"
	"github.com/zhenhuasu/mgame/game/model"
	"github.com/zhenhuasu/mgame/game/service"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("game.handler")

// GuildSender game→guild 发送接口（guildclient.Pool 实现；nil = 无公会服务）
type GuildSender interface {
	Send(t *pb.Transmit) bool
}

type RankSender interface {
	UpdateRole(uid int64, role *model.Role) bool
}

// Handler 消息处理单元：持有用户管理器与握手表（事件在单队列内串行，无需加锁）
type Handler struct {
	mgr    *service.UserManager
	secret string // gate↔game 内网密钥，空 = 不校验
	guild  GuildSender
	rank   RankSender

	authed map[int64]bool // 已完成 RegisterGate 握手的 gate 会话
}

// New 创建处理单元
func New(mgr *service.UserManager, gateSecret string, guild GuildSender, rank RankSender) *Handler {
	return &Handler{
		mgr:    mgr,
		secret: gateSecret,
		guild:  guild,
		rank:   rank,
		authed: make(map[int64]bool),
	}
}

// Register 注册全部对内处理函数，须在事件循环起动前调用
func (h *Handler) Register() {
	net.Register("SessionAccepted", h.onAccepted)
	net.Register("SessionClosed", h.onClosed)
	net.Register(&pb.RegisterGate{}, h.onRegisterGate)
	registerPlayerHandlers(h)
	registerForwardHandlers(h)
}

func (h *Handler) isAuthed(ses cellnet.Session) bool {
	return h.authed[ses.ID()]
}

// registerAuthed 注册客户端最终协议处理函数：队列内校验握手与 Transmit 上下文
func (h *Handler) registerAuthed(msg interface{}, fn func(cellnet.Event, svcproc.TransmitContext)) {
	net.Register(msg, func(ev cellnet.Event) {
		rc, ok := svcproc.ContextOf(ev)
		if !ok {
			log.Warnf("drop %T: missing transmit context session=%d", ev.Message(), ev.Session().ID())
			return
		}
		if !h.isAuthed(ev.Session()) {
			log.Warnf("drop %T from unauthenticated session=%d uid=%d", ev.Message(), ev.Session().ID(), rc.UID)
			return
		}
		fn(ev, rc)
	})
}

func (h *Handler) RoleProgressChanged(uid int64, role *model.Role, levelUps int) {
	if h.rank == nil {
		return
	}
	if !h.rank.UpdateRole(uid, role) {
		log.Warnf("rank unavailable uid=%d role=%d level_ups=%d", uid, role.ID, levelUps)
	}
}

func (h *Handler) NotifyRankChange(roleID int64, board pb.RankBoard, oldRank, newRank int32, score int64) {
	log.Infof("rank changed role=%d board=%d old=%d new=%d score=%d", roleID, board, oldRank, newRank, score)
}
