// Package model 领域数据结构（进程内 + 持久化共用）
package model

// User 账号级用户数据，以 Account 为唯一键（Mongo users 集合）。
// users 文档只存账号信息，角色归属由 roles.account 反查派生。
type User struct {
	Account string `json:"account"` // 唯一键
	UID     int64  `json:"uid"`     // 最近登录 uid（token 校验结果）
	IP      string `json:"ip"`      // 最近登录出口 IP
	Version string `json:"version"` // 最近登录客户端版本号
	// RoleIDs 由 roles 集合派生（不落库），仅进程内使用
	RoleIDs []int64 `json:"role_ids"`
	// OnlineRole 当前在线角色；仅内存，不落库（game 重启后需重新 EnterGame）
	OnlineRole *Role `json:"online_role,omitempty"`
}

// Role 角色（Mongo roles 集合，role_id 全局唯一、同账号内 name 唯一）
type Role struct {
	ID      int64  `json:"id"`
	Account string `json:"account"` // 所属账号
	Name    string `json:"name"`
	Level   int32  `json:"level"`
	Avatar  string `json:"avatar"`
	Exp     int64  `json:"exp"`
	Power   int64  `json:"power"`
}
