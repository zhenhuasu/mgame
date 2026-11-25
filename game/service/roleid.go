package service

import (
	"context"

	"github.com/zhenhuasu/mgame/core/redis"
)

// DefaultRoleIDKey 全局角色 ID 自增键
const DefaultRoleIDKey = "mgame:role:id"

// RoleIDGen 全局角色 ID 生成：Redis INCR 单命令原子自增，
// 天然并发安全，无需分布式锁；生成后若写入失败只留空洞（可接受）。
type RoleIDGen struct {
	kv  *redis.Client
	key string
}

// NewRoleIDGen key 为空时用 DefaultRoleIDKey
func NewRoleIDGen(kv *redis.Client, key string) *RoleIDGen {
	if key == "" {
		key = DefaultRoleIDKey
	}
	return &RoleIDGen{kv: kv, key: key}
}

// Next 取下一个全局唯一角色 ID（>=1）
func (g *RoleIDGen) Next(ctx context.Context) (int64, error) {
	return g.kv.Incr(ctx, g.key, 1)
}
