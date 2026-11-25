// Package redis 封装 Redis 连接与基础操作（get/set/del/incr），
// 供各服务共用：读缓存、全局自增键等。业务语义（缓存策略等）由调用方负责。
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/zhenhuasu/mgame/config"
)

// Client Redis 客户端（薄封装，必要时可 Raw 取原始客户端）
type Client struct {
	c *goredis.Client
}

// New 建连并 Ping 校验（3s 超时）；失败返回 error，调用方按需 fail-fast
func New(cfg config.Redis) (*Client, error) {
	c := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping %s: %w", cfg.Addr, err)
	}
	return &Client{c: c}, nil
}

// Get 读取；不存在返回 (nil, false, nil)
func (c *Client) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := c.c.Get(ctx, key).Bytes()
	if err == goredis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

// Set 写入；ttl 为 0 表示不过期
func (c *Client) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return c.c.Set(ctx, key, val, ttl).Err()
}

// Del 删除键（不存在视为成功）
func (c *Client) Del(ctx context.Context, key string) error {
	return c.c.Del(ctx, key).Err()
}

// Incr 原子自增 delta 并返回新值（用于全局唯一 ID 生成，无需分布式锁）
func (c *Client) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return c.c.IncrBy(ctx, key, delta).Result()
}

// Raw 返回原始客户端（逃生口，供未封装的命令使用）
func (c *Client) Raw() *goredis.Client { return c.c }

// Close 释放连接
func (c *Client) Close() error { return c.c.Close() }
