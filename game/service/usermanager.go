// Package service 业务逻辑：在线用户管理、角色创建与进入游戏
package service

import (
	"context"
	"fmt"

	cellnet "github.com/davyxu/cellnet"
	golog "github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/game/model"
	"github.com/zhenhuasu/mgame/game/repo"
)

var log = golog.New("game.service")

// OnlineUser 在线用户（进程内表；本实例事件单队列串行访问）
type OnlineUser struct {
	UID     int64
	Account string
	User    *model.User
	Session cellnet.Session // 所属 gate 连接
}

// UserManager 在线用户管理 + 角色业务（依赖仓储与全局 ID 生成）
type UserManager struct {
	repo           repo.UserRepo
	idgen          *RoleIDGen
	autoCreateRole bool
	users          map[int64]*OnlineUser // uid → 在线用户
}

// NewUserManager autoCreateRole：允许 EnterGame(role_id=0) 在无角色时自动建角
func NewUserManager(r repo.UserRepo, idgen *RoleIDGen, autoCreateRole bool) *UserManager {
	return &UserManager{
		repo:           r,
		idgen:          idgen,
		autoCreateRole: autoCreateRole,
		users:          make(map[int64]*OnlineUser),
	}
}

// Login 加载/创建账号并刷新登录信息，登记在线。
// 有角色时再取角色基本信息一并返回（登录回包用）。
func (m *UserManager) Login(ctx context.Context, ses cellnet.Session, uid int64, account, ip, version string) (*model.User, []model.Role, error) {
	u, err := m.repo.Login(ctx, account, uid, ip, version)
	if err != nil {
		return nil, nil, err
	}
	var roles []model.Role
	if len(u.RoleIDs) > 0 {
		roles, err = m.repo.LoadRoles(ctx, account)
		if err != nil {
			return nil, nil, err
		}
	}
	m.users[uid] = &OnlineUser{UID: uid, Account: account, User: u, Session: ses}
	return u, roles, nil
}

// MarkOnline gate 重连/断线恢复后补登记；内存缺失时从仓储恢复（RoleIDs 由 roles 派生，
// OnlineRole 不落库故为 nil，需客户端重新 EnterGame）
func (m *UserManager) MarkOnline(ctx context.Context, uid int64, account string, ses cellnet.Session) error {
	if ou, ok := m.users[uid]; ok {
		ou.Session = ses
		return nil
	}
	u, ok, err := m.repo.Load(ctx, account)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("user %q not found", account)
	}
	m.users[uid] = &OnlineUser{UID: uid, Account: account, User: u, Session: ses}
	return nil
}

// Offline 下线：仅清理内存与在线记录（在线角色不落库，无需写库）
func (m *UserManager) Offline(uid int64) {
	ou, ok := m.users[uid]
	if !ok {
		return
	}
	delete(m.users, uid)
	ou.User.OnlineRole = nil
}

// RemoveBySession gate 连接断开：仅摘除在线记录（不清在线角色，gate 重连会补登记）
func (m *UserManager) RemoveBySession(sessionID int64) []int64 {
	var removed []int64
	for uid, ou := range m.users {
		if ou.Session != nil && ou.Session.ID() == sessionID {
			delete(m.users, uid)
			removed = append(removed, uid)
		}
	}
	return removed
}

// Get 取在线用户
func (m *UserManager) Get(uid int64) (*OnlineUser, bool) {
	ou, ok := m.users[uid]
	return ou, ok
}

// CreateRole 创建角色：全局 ID（Redis INCR）→ 仓储插入 → 回填内存 RoleIDs。
// name 为空回退账号名（同账号内唯一，由 Mongo 唯一索引兜底）。
func (m *UserManager) CreateRole(ctx context.Context, uid int64, name string) (*model.Role, error) {
	ou, ok := m.users[uid]
	if !ok {
		return nil, repo.ErrNotOnline
	}
	if name == "" {
		name = ou.Account
	}
	id, err := m.idgen.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("role id: %w", err)
	}
	r := &model.Role{ID: id, Account: ou.Account, Name: name, Level: 1}
	if err := m.repo.CreateRole(ctx, r); err != nil {
		return nil, err
	}
	ou.User.RoleIDs = append(ou.User.RoleIDs, r.ID)
	return r, nil
}

// EnterGame 进入游戏：加载角色完整数据并置为在线角色（仅内存）。
// roleID=0 仅当开关开启且账号尚无角色时自动创建默认角色。
func (m *UserManager) EnterGame(ctx context.Context, uid int64, roleID int64) (*model.Role, error) {
	ou, ok := m.users[uid]
	if !ok {
		return nil, repo.ErrNotOnline
	}
	if roleID == 0 {
		if !m.autoCreateRole {
			return nil, fmt.Errorf("role_id required")
		}
		if len(ou.User.RoleIDs) > 0 {
			return nil, fmt.Errorf("role_id required")
		}
		return m.CreateRole(ctx, uid, ou.Account)
	}

	// 按 account 限定归属，加载完整角色数据
	role, ok, err := m.repo.LoadRole(ctx, ou.Account, roleID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("role %d not found", roleID)
	}
	ou.User.OnlineRole = role
	return role, nil
}
