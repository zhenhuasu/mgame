// Package model 公会领域结构
package model

import "time"

// 职位
const (
	PositionLeader int32 = 1 // 会长
	PositionVice   int32 = 2 // 副会长/管理员
	PositionMember int32 = 3 // 成员
)

// 加入方式
const (
	JoinOpen       int32 = 0 // 公开直接加入
	JoinInviteOnly int32 = 1 // 仅邀请加入
)

// 事件类型（与 proto GuildEventKind 对应）
const (
	EventKicked    int32 = 1
	EventDisbanded int32 = 2
	EventTransfer  int32 = 3
	EventPosition  int32 = 4
)

// DefaultLevel 公会等级占位
const DefaultLevel int32 = 1

// Guild 公会
type Guild struct {
	ID           int64
	Name         string
	LeaderRoleID int64
	Notice       string
	Level        int32
	JoinMode     int32
	MemberCount  int32
	CreatedAt    time.Time
}

// GuildMember 公会成员（role_id 全局唯一 = 一角色一公会）
type GuildMember struct {
	GuildID   int64
	RoleID    int64
	RoleName  string
	RoleLevel int32
	Position  int32
	JoinedAt  time.Time
}

// GuildInvite 入会邀请（guild_id + role_id 唯一）
type GuildInvite struct {
	GuildID       int64
	RoleID        int64
	InvitedBy     int64
	InvitedByName string
	CreatedAt     time.Time
}
