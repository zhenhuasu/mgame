// Package handler game 服务消息处理：gate 握手、玩家生命周期、角色与业务转发。
// 由 network.Listen 在起事件循环前调用 Register 注册。
package handler

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/game/service"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("game.handler")

// Handler 消息处理单元：持有用户管理器与握手表（事件在单队列内串行，无需加锁）
type Handler struct {
	mgr    *service.UserManager
	secret string // gate↔game 内网密钥，空 = 不校验

	authed map[int64]bool // 已完成 RegisterGate 握手的 gate 会话
}

// New 创建处理单元
func New(mgr *service.UserManager, gateSecret string) *Handler {
	return &Handler{
		mgr:    mgr,
		secret: gateSecret,
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
