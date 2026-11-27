// Package repo 公会持久化：Mongo 为唯一持久层（guilds/members/invites 三集合）。
package repo

import (
	"context"
	"errors"

	"github.com/zhenhuasu/mgame/guild/model"
)

// 仓储层/业务错误（handler 转 code=1 reason）
var (
	ErrGuildNameExists   = errors.New("guild name already exists")
	ErrGuildNotFound     = errors.New("guild not found")
	ErrAlreadyInGuild    = errors.New("already in guild")
	ErrNotInGuild        = errors.New("not in guild")
	ErrGuildFull         = errors.New("guild is full")
	ErrNoPermission      = errors.New("permission denied")
	ErrLeaderCannotLeave = errors.New("leader cannot leave")
	ErrInviteNotFound    = errors.New("invite not found")
	ErrJoinClosed        = errors.New("join closed (invite only)")
	ErrInvalidArgument   = errors.New("invalid argument")
)

// GuildRepo 公会仓储
type GuildRepo interface {
	// EnsureIndexes 创建唯一索引（启动时调用一次）
	EnsureIndexes(ctx context.Context) error

	// Create 插入公会并加入会长成员（失败回滚）；重名返回 ErrGuildNameExists
	Create(ctx context.Context, g *model.Guild, leaderName string, leaderLevel int32, maxMembers int) error
	// Get 按 guild_id 读取；不存在返回 (nil,false,nil)
	Get(ctx context.Context, guildID int64) (*model.Guild, bool, error)
	// List 读公会列表（按 guild_id 升序，最多 limit 条）
	List(ctx context.Context, limit int64) ([]model.Guild, error)
	// SetNotice 修改公告
	SetNotice(ctx context.Context, guildID int64, notice string) error
	// SetJoinMode 修改加入方式
	SetJoinMode(ctx context.Context, guildID int64, mode int32) error
	// SetLeader 修改会长 role_id
	SetLeader(ctx context.Context, guildID int64, roleID int64) error
	// Delete 删除公会文档（成员/邀请由调用方处理）
	Delete(ctx context.Context, guildID int64) error

	// AddMember 加入成员：插入成员 + 条件自增 member_count（member_count<max），
	// 已满回滚并返回 ErrGuildFull；已在该公会返回 ErrAlreadyInGuild
	AddMember(ctx context.Context, guildID, roleID int64, name string, level, position int32, maxMembers int) error
	// RemoveMember 移除成员并自减 member_count；不存在返回 (false,nil)
	RemoveMember(ctx context.Context, guildID, roleID int64) (bool, error)
	// DeleteMembers 删除公会全部成员，返回删除数量
	DeleteMembers(ctx context.Context, guildID int64) (int64, error)
	// GetMember 按 role_id 查成员（含公会归属）；不存在返回 (nil,false,nil)
	GetMember(ctx context.Context, roleID int64) (*model.GuildMember, bool, error)
	// GetGuildMember 查指定公会内的成员；不存在返回 (nil,false,nil)
	GetGuildMember(ctx context.Context, guildID, roleID int64) (*model.GuildMember, bool, error)
	// ListMembers 列出公会成员（按 joined_at 升序）
	ListMembers(ctx context.Context, guildID int64) ([]model.GuildMember, error)
	// SetMemberPosition 修改成员职位
	SetMemberPosition(ctx context.Context, guildID, roleID int64, position int32) error

	// UpsertInvite 写入邀请（已存在则忽略）
	UpsertInvite(ctx context.Context, inv *model.GuildInvite) error
	// GetInvite 读取邀请；不存在返回 (nil,false,nil)
	GetInvite(ctx context.Context, guildID, roleID int64) (*model.GuildInvite, bool, error)
	// ListInvites 列出某角色的待处理邀请（按 created_at 降序）
	ListInvites(ctx context.Context, roleID int64) ([]model.GuildInvite, error)
	// DeleteInvite 删除单条邀请；不存在返回 (false,nil)
	DeleteInvite(ctx context.Context, guildID, roleID int64) (bool, error)
	// DeleteInvitesByRole 删除某角色的全部邀请，返回删除数量
	DeleteInvitesByRole(ctx context.Context, roleID int64) (int64, error)
	// DeleteInvitesByGuild 删除某公会的全部邀请，返回删除数量
	DeleteInvitesByGuild(ctx context.Context, guildID int64) (int64, error)
}
