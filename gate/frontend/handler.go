package frontend

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/gate/session"

	pb "github.com/zhenhuasu/mgame/proto"
)

// GameRouter 前端所需的后端路由能力（由 backend.Pool 实现）。
// frontend 只依赖此接口，不 import backend，避免循环依赖。
type GameRouter interface {
	// Assign 轮询分配 game 实例，返回 (新地址, 旧地址, 是否成功)
	Assign(uid int64, account string) (string, string, bool)
	// Unassign 移除路由，返回原地址
	Unassign(uid int64) (string, bool)
	// RouteOf 查询 uid 当前路由的 game 地址
	RouteOf(uid int64) (string, bool)
	// SendTo 向指定 game 地址发消息
	SendTo(addr string, msg interface{}) bool
}

// Server 客户端侧处理单元：持有登录态与后端路由，handler 均为其方法，
// 由 net.Register 绑定到统一 dispatch。
type Server struct {
	cfg   *config.Gate
	mgr   *session.Manager
	games GameRouter
}

// New 创建客户端侧处理单元
func New(cfg *config.Gate, mgr *session.Manager, games GameRouter) *Server {
	return &Server{cfg: cfg, mgr: mgr, games: games}
}

// registerHandlers 注册 ws 侧处理函数（含 SessionClosed 分流，须在起循环前调用）
func (s *Server) registerHandlers() {
	net.Register("SessionAccepted", s.onAccepted)
	net.Register("SessionClosed", s.onClosed)
	net.Register(&pb.PingReq{}, s.onPing)
	net.Register(&pb.LoginReq{}, s.onLogin)
	net.Register(&pb.LogoutReq{}, s.onLogout)
	// 联调后门仅在 dev 模式注册；生产（jwt）不暴露客户端广播入口
	if debugHandlers {
		net.Register(&pb.BroadcastReq{}, s.onBroadcast)
	}
	// 未显式注册的业务消息 → 经所属 game 转发
	net.RegisterDefault(s.forwardC2S)
}

// onAccepted 客户端连接建立
func (s *Server) onAccepted(ev cellnet.Event) {
	log.Infof("session accepted id=%d", ev.Session().ID())
}

// onClosed 客户端断开。SessionClosed 为全局 key（game 连接断开也命中），
// 按 peer 类型分流，只处理 ws 客户端会话。
func (s *Server) onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "gorillaws.Acceptor" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)

	if uid, ok := s.mgr.UIDBySession(ev.Session().ID()); ok {
		if addr, ok := s.games.Unassign(uid); ok {
			s.games.SendTo(addr, &pb.PlayerOffline{Uid: uid, Reason: 1})
		}
	}
	s.mgr.RemoveBySession(ev.Session().ID())
	log.Infof("session closed id=%d reason=%v", ev.Session().ID(), msg.Reason)
}

// onPing 心跳回包
func (s *Server) onPing(ev cellnet.Event) {
	msg := ev.Message().(*pb.PingReq)
	ses := ev.Session()
	log.Debugf("recv PingReq id=%d ts=%d", ses.ID(), msg.Timestamp)
	ses.Send(&pb.PingAck{
		Timestamp: msg.Timestamp,
		SessionId: ses.ID(),
	})
}

// onLogin 登录
func (s *Server) onLogin(ev cellnet.Event) {
	s.handleLogin(ev, ev.Message().(*pb.LoginReq))
}

// onLogout 退出登录
func (s *Server) onLogout(ev cellnet.Event) {
	s.handleLogout(ev)
}

// onBroadcast 联调用：客户端触发广播
func (s *Server) onBroadcast(ev cellnet.Event) {
	s.handleBroadcast(ev, ev.Message().(*pb.BroadcastReq))
}

func (s *Server) handleLogin(ev cellnet.Event, req *pb.LoginReq) {
	ses := ev.Session()

	// uid/account 以校验结果为准，不信任客户端自报的 uid
	uid, account, err := VerifyToken(req.Account, req.Uid, req.Token)
	if err != nil {
		log.Infof("login rejected session=%d account=%s uid=%d: %v", ses.ID(), req.Account, req.Uid, err)
		ses.Send(&pb.LoginAck{Code: 1, Reason: err.Error()})
		return
	}

	old := s.mgr.Login(ses, &session.LoginInfo{
		Account: account,
		UID:     uid,
	})

	// 顶号：先通知旧连接再关闭（Send 走发送队列，KickNtf 会先于 Close 冲刷）
	if old != nil {
		log.Infof("duplicate login uid=%d, kick old session=%d", uid, old.ID())
		old.Send(&pb.KickNtf{Reason: "duplicate login"})
		old.Close()
	}

	// 分配 game 实例并上报在线（顶号迁移到不同实例时先向旧实例下线）
	if addr, prev, ok := s.games.Assign(uid, account); ok {
		if prev != "" && prev != addr {
			s.games.SendTo(prev, &pb.PlayerOffline{Uid: uid, Reason: 2})
		}
		s.games.SendTo(addr, &pb.PlayerOnline{Uid: uid, Account: account})
		log.Infof("assign game uid=%d addr=%s", uid, addr)
	} else {
		log.Warnf("no game available uid=%d", uid)
	}

	log.Infof("login ok session=%d account=%s uid=%d online=%d", ses.ID(), account, uid, s.mgr.OnlineCount())
	ses.Send(&pb.LoginAck{Code: 0, Uid: uid})
}

// requireLogin 校验会话已登录且未过期；过期则踢下线。通过时刷新活跃时间。
func (s *Server) requireLogin(ev cellnet.Event) (int64, bool) {
	ses := ev.Session()
	uid, ok := s.mgr.UIDBySession(ses.ID())
	if !ok {
		return 0, false
	}
	if s.mgr.Expired(ses.ID()) {
		log.Infof("session expired id=%d uid=%d", ses.ID(), uid)
		if addr, ok := s.games.Unassign(uid); ok {
			s.games.SendTo(addr, &pb.PlayerOffline{Uid: uid, Reason: 1})
		}
		s.mgr.RemoveBySession(ses.ID())
		ses.Send(&pb.KickNtf{Reason: "session expired"})
		ses.Close()
		return 0, false
	}
	s.mgr.Touch(ses.ID())
	return uid, true
}

func (s *Server) handleLogout(ev cellnet.Event) {
	ses := ev.Session()

	info := s.mgr.Logout(ses)
	if info == nil {
		ses.Send(&pb.LogoutAck{Code: 1, Reason: "not logged in"})
		return
	}

	if addr, ok := s.games.Unassign(info.UID); ok {
		s.games.SendTo(addr, &pb.PlayerOffline{Uid: info.UID, Reason: 0})
	}

	log.Infof("logout session=%d uid=%d online=%d", ses.ID(), info.UID, s.mgr.OnlineCount())
	ses.Send(&pb.LogoutAck{Code: 0})
}

// forwardC2S 未被 gate 处理的业务消息 → 编码后经所属 game 转发（net 默认处理）
func (s *Server) forwardC2S(ev cellnet.Event) {
	ses := ev.Session()
	msg := ev.Message()

	uid, ok := s.requireLogin(ev)
	if !ok {
		log.Debugf("drop unlogged/expired msg %T session=%d", msg, ses.ID())
		return
	}
	addr, ok := s.games.RouteOf(uid)
	if !ok {
		log.Warnf("no game route uid=%d msg=%T", uid, msg)
		return
	}
	fwd, err := EncodeForward(uid, msg)
	if err != nil {
		log.Errorf("encode forward uid=%d msg=%T: %v", uid, msg, err)
		return
	}
	if !s.games.SendTo(addr, fwd) {
		log.Warnf("game unavailable addr=%s uid=%d msg=%T", addr, uid, msg)
	}
}

// handleBroadcast 联调用：客户端触发广播。
// TODO 上线前移除或加权限校验。
func (s *Server) handleBroadcast(ev cellnet.Event, req *pb.BroadcastReq) {
	ses := ev.Session()

	if _, ok := s.requireLogin(ev); !ok {
		ses.Send(&pb.BroadcastAck{Count: 0})
		return
	}

	count := s.mgr.BroadcastToUids(req.Uids, &pb.NotifyNtf{Content: req.Content})
	log.Infof("broadcast from session=%d uids=%v count=%d content=%q", ses.ID(), req.Uids, count, req.Content)
	ses.Send(&pb.BroadcastAck{Count: int32(count)})
}

// EncodeForward 将业务消息编码为 ForwardC2S 信封
func EncodeForward(uid int64, msg interface{}) (*pb.ForwardC2S, error) {
	data, meta, err := codec.EncodeMessage(msg, nil)
	if err != nil {
		return nil, err
	}
	return &pb.ForwardC2S{Uid: uid, MsgId: uint32(meta.ID), Payload: data}, nil
}
