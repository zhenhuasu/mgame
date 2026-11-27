package service

import (
	"context"

	"github.com/zhenhuasu/mgame/core/redis"
)

// DefaultGuildIDKey 全局公会 ID 自增键
const DefaultGuildIDKey = "mgame:guild:id"

// GuildIDGen 全局公会 ID 生成：Redis INCR 单命令原子自增，无需分布式锁；
// 生成后若写入失败只留空洞（可接受）。
type GuildIDGen struct {
	kv  *redis.Client
	key string
}

// NewGuildIDGen key 为空时用 DefaultGuildIDKey
func NewGuildIDGen(kv *redis.Client, key string) *GuildIDGen {
	if key == "" {
		key = DefaultGuildIDKey
	}
	return &GuildIDGen{kv: kv, key: key}
}

// Next 取下一个全局唯一公会 ID（>=1）
func (g *GuildIDGen) Next(ctx context.Context) (int64, error) {
	return g.kv.Incr(ctx, g.key, 1)
}
