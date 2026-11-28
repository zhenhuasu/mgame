// Package repo 用户/角色持久化：Mongo 为唯一持久层（users/roles 两集合），
// Redis 只做读缓存（cache.go 装饰器）。
package repo

import (
	"context"
	"errors"

	"github.com/zhenhuasu/mgame/game/model"
)

// 仓储层错误（handler 转为协议回包）
var (
	// ErrNotOnline 用户不在本实例在线表
	ErrNotOnline = errors.New("not online")
	// ErrRoleNameExists 角色名全局重复（Mongo 唯一索引 name 拦截）
	ErrRoleNameExists = errors.New("role name already exists")
)

// UserRepo 用户/角色仓储
type UserRepo interface {
	// Login 确保账号文档存在，刷新 uid/ip/version 后返回用户（RoleIDs 由 roles 派生）
	Login(ctx context.Context, account string, uid int64, ip, version string) (*model.User, error)
	// Load 读取用户；不存在返回 (nil, false, nil)
	Load(ctx context.Context, account string) (*model.User, bool, error)
	// LoadRoles 读取账号下角色基本信息（role_id/account/name/level，按 role_id 升序），供登录回包
	LoadRoles(ctx context.Context, account string) ([]model.Role, error)
	// LoadRole 读取单个角色完整数据（按 account 限定归属）；不存在返回 (nil, false, nil)
	LoadRole(ctx context.Context, account string, roleID int64) (*model.Role, bool, error)
	// LoadRoleByName 按角色名（全局唯一）读取角色；不存在返回 (nil, false, nil)
	LoadRoleByName(ctx context.Context, name string) (*model.Role, bool, error)
	// CreateRole 插入角色（role.Account 为归属账号，role.ID 由调用方生成）；
	// 重名返回 ErrRoleNameExists
	CreateRole(ctx context.Context, role *model.Role) error
	// UpdateRole 更新角色成长字段（按 role_id + account 限定归属）
	UpdateRole(ctx context.Context, role *model.Role) error
	// EnsureIndexes 创建唯一索引，失败返回 error（启动时调用一次）
	EnsureIndexes(ctx context.Context) error
}
