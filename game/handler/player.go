package handler

import (
	"context"
	"time"

	"github.com/davyxu/cellnet"

	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/game/model"

	pb "github.com/zhenhuasu/mgame/proto"
)

// registerPlayerHandlers 注册玩家生命周期处理函数
func registerPlayerHandlers(h *Handler) {
	net.Register(&pb.PlayerLogin{}, h.onPlayerLogin)
	net.Register(&pb.PlayerOnline{}, h.onPlayerOnline)
	net.Register(&pb.PlayerOffline{}, h.onPlayerOffline)
}

// onPlayerLogin 客户端登录：以 account 加载用户，回角色列表
func (h *Handler) onPlayerLogin(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerLogin)
	ses := ev.Session()
	if !h.isAuthed(ses) {
		log.Warnf("drop PlayerLogin from unauthenticated session=%d uid=%d", ses.ID(), msg.Uid)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u, roles, err := h.mgr.Login(ctx, ses, msg.Uid, msg.Account, msg.Ip, msg.Version)
	if err != nil {
		log.Errorf("login user account=%s uid=%d: %v", msg.Account, msg.Uid, err)
		ses.Send(&pb.PlayerLoginAck{Uid: msg.Uid, Code: 1, Reason: err.Error()})
		return
	}

	log.Infof("login ok uid=%d account=%s ip=%s version=%s roles=%d",
		msg.Uid, u.Account, u.IP, u.Version, len(roles))
	ses.Send(&pb.PlayerLoginAck{Uid: msg.Uid, Code: 0, Roles: toRoleInfo(roles)})
}

// onPlayerOnline 玩家上线（断线重连后 gate 补登记，内存缺失时从仓储恢复）
func (h *Handler) onPlayerOnline(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerOnline)
	if !h.isAuthed(ev.Session()) {
		log.Warnf("drop PlayerOnline from unauthenticated session=%d uid=%d", ev.Session().ID(), msg.Uid)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.mgr.MarkOnline(ctx, msg.Uid, msg.Account, ev.Session()); err != nil {
		log.Errorf("mark online uid=%d account=%s: %v", msg.Uid, msg.Account, err)
		return
	}
	log.Infof("player online uid=%d account=%s", msg.Uid, msg.Account)
}

// onPlayerOffline 玩家下线：清理内存在线记录（在线角色不落库，无需写库）
func (h *Handler) onPlayerOffline(ev cellnet.Event) {
	msg := ev.Message().(*pb.PlayerOffline)
	if !h.isAuthed(ev.Session()) {
		log.Warnf("drop PlayerOffline from unauthenticated session=%d uid=%d", ev.Session().ID(), msg.Uid)
		return
	}
	h.mgr.Offline(msg.Uid)
	log.Infof("player offline uid=%d reason=%d", msg.Uid, msg.Reason)
}

// toRoleInfo 领域模型 → 协议结构
func toRoleInfo(roles []model.Role) []*pb.RoleInfo {
	out := make([]*pb.RoleInfo, 0, len(roles))
	for _, r := range roles {
		out = append(out, &pb.RoleInfo{RoleId: r.ID, Name: r.Name, Level: r.Level})
	}
	return out
}

// toRoleInfo1 单角色转换（nil → nil）
func toRoleInfo1(r *model.Role) *pb.RoleInfo {
	if r == nil {
		return nil
	}
	return &pb.RoleInfo{RoleId: r.ID, Name: r.Name, Level: r.Level}
}
