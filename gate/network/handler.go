package network

import (
	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

// mgr 网关登录态（单进程单实例）
var mgr = NewManager()

// registerWsHandlers 注册 ws 客户端侧处理函数。
// 须在 net.Run 起循环前调用；与 game 连接侧共用 net 统一 dispatch。
func registerWsHandlers() {
	net.Register("SessionAccepted", onAccepted)
	net.Register("SessionClosed", onClosed)
	net.Register(&pb.PingReq{}, onPing)
	net.Register(&pb.LoginReq{}, onLogin)
	net.Register(&pb.LogoutReq{}, onLogout)
	// 联调后门仅在 dev 模式注册；生产（jwt）不暴露客户端广播入口
	if debugHandlers {
		net.Register(&pb.BroadcastReq{}, onBroadcast)
	}
	// 未显式注册的业务消息 → 经所属 game 转发
	net.RegisterDefault(forwardC2S)
}

// onAccepted 客户端连接建立
func onAccepted(ev cellnet.Event) {
	log.Infof("session accepted id=%d", ev.Session().ID())
}

// onClosed 客户端断开。SessionClosed 为全局 key（game 连接断开也命中），
// 按 peer 类型分流，只处理 ws 客户端会话。
func onClosed(ev cellnet.Event) {
	if ev.Session().Peer().TypeName() != "gorillaws.Acceptor" {
		return
	}
	msg := ev.Message().(*cellnet.SessionClosed)

	if uid, ok := mgr.UIDBySession(ev.Session().ID()); ok {
		if addr, ok := games.Unassign(uid); ok {
			games.SendTo(addr, &pb.PlayerOffline{Uid: uid, Reason: 1})
		}
	}
	mgr.RemoveBySession(ev.Session().ID())
	log.Infof("session closed id=%d reason=%v", ev.Session().ID(), msg.Reason)
}

// onPing 心跳回包
func onPing(ev cellnet.Event) {
	msg := ev.Message().(*pb.PingReq)
	ses := ev.Session()
	log.Debugf("recv PingReq id=%d ts=%d", ses.ID(), msg.Timestamp)
	ses.Send(&pb.PingAck{
		Timestamp: msg.Timestamp,
		SessionId: ses.ID(),
	})
}

// onLogin 登录
func onLogin(ev cellnet.Event) {
	handleLogin(ev, ev.Message().(*pb.LoginReq))
}

// onLogout 退出登录
func onLogout(ev cellnet.Event) {
	handleLogout(ev)
}

// onBroadcast 联调用：客户端触发广播
func onBroadcast(ev cellnet.Event) {
	handleBroadcast(ev, ev.Message().(*pb.BroadcastReq))
}

func handleLogin(ev cellnet.Event, req *pb.LoginReq) {
	ses := ev.Session()

	// uid/account 以校验结果为准，不信任客户端自报的 uid
	uid, account, err := VerifyToken(req.Account, req.Uid, req.Token)
	if err != nil {
		log.Infof("login rejected session=%d account=%s uid=%d: %v", ses.ID(), req.Account, req.Uid, err)
		ses.Send(&pb.LoginAck{Code: 1, Reason: err.Error()})
		return
	}

	old := mgr.Login(ses, &LoginInfo{
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
	if addr, prev, ok := games.Assign(uid, account); ok {
		if prev != "" && prev != addr {
			games.SendTo(prev, &pb.PlayerOffline{Uid: uid, Reason: 2})
		}
		games.SendTo(addr, &pb.PlayerOnline{Uid: uid, Account: account})
		log.Infof("assign game uid=%d addr=%s", uid, addr)
	} else {
		log.Warnf("no game available uid=%d", uid)
	}

	log.Infof("login ok session=%d account=%s uid=%d online=%d", ses.ID(), account, uid, mgr.OnlineCount())
	ses.Send(&pb.LoginAck{Code: 0, Uid: uid})
}

// requireLogin 校验会话已登录且未过期；过期则踢下线。通过时刷新活跃时间。
func requireLogin(ev cellnet.Event) (int64, bool) {
	ses := ev.Session()
	uid, ok := mgr.UIDBySession(ses.ID())
	if !ok {
		return 0, false
	}
	if mgr.Expired(ses.ID()) {
		log.Infof("session expired id=%d uid=%d", ses.ID(), uid)
		if addr, ok := games.Unassign(uid); ok {
			games.SendTo(addr, &pb.PlayerOffline{Uid: uid, Reason: 1})
		}
		mgr.RemoveBySession(ses.ID())
		ses.Send(&pb.KickNtf{Reason: "session expired"})
		ses.Close()
		return 0, false
	}
	mgr.Touch(ses.ID())
	return uid, true
}

func handleLogout(ev cellnet.Event) {
	ses := ev.Session()

	info := mgr.Logout(ses)
	if info == nil {
		ses.Send(&pb.LogoutAck{Code: 1, Reason: "not logged in"})
		return
	}

	if addr, ok := games.Unassign(info.UID); ok {
		games.SendTo(addr, &pb.PlayerOffline{Uid: info.UID, Reason: 0})
	}

	log.Infof("logout session=%d uid=%d online=%d", ses.ID(), info.UID, mgr.OnlineCount())
	ses.Send(&pb.LogoutAck{Code: 0})
}

// forwardC2S 未被 gate 处理的业务消息 → 编码后经所属 game 转发（net 默认处理）
func forwardC2S(ev cellnet.Event) {
	ses := ev.Session()
	msg := ev.Message()

	uid, ok := requireLogin(ev)
	if !ok {
		log.Debugf("drop unlogged/expired msg %T session=%d", msg, ses.ID())
		return
	}
	addr, ok := games.RouteOf(uid)
	if !ok {
		log.Warnf("no game route uid=%d msg=%T", uid, msg)
		return
	}
	fwd, err := EncodeForward(uid, msg)
	if err != nil {
		log.Errorf("encode forward uid=%d msg=%T: %v", uid, msg, err)
		return
	}
	if !games.SendTo(addr, fwd) {
		log.Warnf("game unavailable addr=%s uid=%d msg=%T", addr, uid, msg)
	}
}

// handleBroadcast 联调用：客户端触发广播。
// TODO 上线前移除或加权限校验。
func handleBroadcast(ev cellnet.Event, req *pb.BroadcastReq) {
	ses := ev.Session()

	if _, ok := requireLogin(ev); !ok {
		ses.Send(&pb.BroadcastAck{Count: 0})
		return
	}

	count := mgr.BroadcastToUids(req.Uids, &pb.NotifyNtf{Content: req.Content})
	log.Infof("broadcast from session=%d uids=%v count=%d content=%q", ses.ID(), req.Uids, count, req.Content)
	ses.Send(&pb.BroadcastAck{Count: int32(count)})
}
