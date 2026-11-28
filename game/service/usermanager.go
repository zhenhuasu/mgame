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

type RoleRules struct {
	ExpBase       int64
	PowerPerLevel int64
	MaxLevel      int32
}

type ProgressNotifier interface {
	RoleProgressChanged(uid int64, role *model.Role, levelUps int)
}

// UserManager 在线用户管理 + 角色业务（依赖仓储与全局 ID 生成）
type UserManager struct {
	repo           repo.UserRepo
	idgen          *RoleIDGen
	autoCreateRole bool
	rules          RoleRules
	notifier       ProgressNotifier
	users          map[int64]*OnlineUser // uid → 在线用户
}

// NewUserManager autoCreateRole：允许 EnterGame(role_id=0) 在无角色时自动建角
func NewUserManager(r repo.UserRepo, idgen *RoleIDGen, autoCreateRole bool, rules RoleRules) *UserManager {
	return &UserManager{
		repo:           r,
		idgen:          idgen,
		autoCreateRole: autoCreateRole,
		rules:          rules,
		users:          make(map[int64]*OnlineUser),
	}
}

func (m *UserManager) SetProgressNotifier(n ProgressNotifier) { m.notifier = n }

func (m *UserManager) notifyProgress(uid int64, role *model.Role, levelUps int) {
	if m.notifier != nil {
		m.notifier.RoleProgressChanged(uid, role, levelUps)
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

// OnlineRole 取用户当前在线角色（未 EnterGame 返回 false）
func (m *UserManager) OnlineRole(uid int64) (*model.Role, bool) {
	ou, ok := m.users[uid]
	if !ok || ou.User.OnlineRole == nil {
		return nil, false
	}
	return ou.User.OnlineRole, true
}

// RoleByName 按角色名（全局唯一）查角色
func (m *UserManager) RoleByName(ctx context.Context, name string) (*model.Role, bool, error) {
	return m.repo.LoadRoleByName(ctx, name)
}

// FindOnlineByRole 按在线角色 ID 查所属 uid（公会推送命中）
func (m *UserManager) FindOnlineByRole(roleID int64) (int64, bool) {
	for uid, ou := range m.users {
		if ou.User.OnlineRole != nil && ou.User.OnlineRole.ID == roleID {
			return uid, true
		}
	}
	return 0, false
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
		role, err := m.CreateRole(ctx, uid, ou.Account)
		if err != nil {
			return nil, err
		}
		ou.User.OnlineRole = role
		m.notifyProgress(uid, role, 0)
		return role, nil
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
	m.notifyProgress(uid, role, 0)
	return role, nil
}

func (m *UserManager) GainExp(ctx context.Context, uid int64, amount int64) (*model.Role, int, error) {
	if amount <= 0 {
		return nil, 0, fmt.Errorf("exp amount must be > 0")
	}
	ou, ok := m.users[uid]
	if !ok || ou.User.OnlineRole == nil {
		return nil, 0, repo.ErrNotOnline
	}
	role := ou.User.OnlineRole
	if role.Exp > (1<<63-1)-amount {
		return nil, 0, fmt.Errorf("exp overflow")
	}

	exp := role.Exp + amount
	level := role.Level
	power := role.Power
	levelUps := 0
	for level < m.rules.MaxLevel {
		need := m.rules.ExpBase * int64(level)
		if need <= 0 || exp < need {
			break
		}
		exp -= need
		level++
		power += m.rules.PowerPerLevel
		levelUps++
	}
	if level >= m.rules.MaxLevel {
		exp = 0
	}

	updated := *role
	updated.Exp = exp
	updated.Level = level
	updated.Power = power
	if err := m.repo.UpdateRole(ctx, &updated); err != nil {
		return nil, 0, err
	}
	*role = updated
	if levelUps > 0 {
		m.notifyProgress(uid, role, levelUps)
	}
	return role, levelUps, nil
}

func (m *UserManager) SetAvatar(ctx context.Context, uid int64, avatar string) (*model.Role, error) {
	ou, ok := m.users[uid]
	if !ok || ou.User.OnlineRole == nil {
		return nil, repo.ErrNotOnline
	}
	role := ou.User.OnlineRole
	updated := *role
	updated.Avatar = avatar
	if err := m.repo.UpdateRole(ctx, &updated); err != nil {
		return nil, err
	}
	*role = updated
	m.notifyProgress(uid, role, 0)
	return role, nil
}
