package repo

import (
	"context"
	"encoding/json"
	"time"

	golog "github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/redis"
	"github.com/zhenhuasu/mgame/game/model"
)

// cached Redis 读缓存装饰器：读穿透 + 写后失效，业务语义全在内层 Mongo 仓储
type cached struct {
	inner  UserRepo
	kv     *redis.Client
	prefix string
	ttl    time.Duration
}

// NewCached 装饰仓储：Load 先查缓存（miss 回源并回填），
// Login/CreateRole 写后 Del 失效（失败仅告警，不阻断业务）
func NewCached(inner UserRepo, kv *redis.Client, prefix string, ttl time.Duration) UserRepo {
	return &cached{inner: inner, kv: kv, prefix: prefix, ttl: ttl}
}

var cacheLog = golog.New("game.repo")

func (c *cached) key(account string) string { return c.prefix + account }

// Load 缓存优先；miss 回源并回填（best effort）
func (c *cached) Load(ctx context.Context, account string) (*model.User, bool, error) {
	if raw, ok, err := c.kv.Get(ctx, c.key(account)); err == nil && ok {
		var u model.User
		jsonErr := json.Unmarshal(raw, &u)
		if jsonErr == nil {
			return &u, true, nil
		}
		cacheLog.Warnf("cache decode %q: %s", account, jsonErr)
	}

	u, found, err := c.inner.Load(ctx, account)
	if err != nil || !found {
		return u, found, err
	}
	if raw, mErr := json.Marshal(u); mErr == nil {
		if sErr := c.kv.Set(ctx, c.key(account), raw, c.ttl); sErr != nil {
			cacheLog.Warnf("cache set %q: %s", account, sErr)
		}
	} else {
		cacheLog.Warnf("cache marshal %q: %s", account, mErr)
	}
	return u, true, nil
}

// Login 走内层写入，随后失效缓存
func (c *cached) Login(ctx context.Context, account string, uid int64, ip, version string) (*model.User, error) {
	u, err := c.inner.Login(ctx, account, uid, ip, version)
	if err != nil {
		return nil, err
	}
	c.drop(ctx, account)
	return u, nil
}

// LoadRoles 直通内层（角色基本信息不缓存，登录时回源）
func (c *cached) LoadRoles(ctx context.Context, account string) ([]model.Role, error) {
	return c.inner.LoadRoles(ctx, account)
}

// LoadRole 直通内层（完整角色不缓存，进游戏时回源）
func (c *cached) LoadRole(ctx context.Context, account string, roleID int64) (*model.Role, bool, error) {
	return c.inner.LoadRole(ctx, account, roleID)
}

// LoadRoleByName 直通内层（公会邀请按名解析，不缓存）
func (c *cached) LoadRoleByName(ctx context.Context, name string) (*model.Role, bool, error) {
	return c.inner.LoadRoleByName(ctx, name)
}

// CreateRole 走内层写入，随后失效缓存
func (c *cached) CreateRole(ctx context.Context, role *model.Role) error {
	if err := c.inner.CreateRole(ctx, role); err != nil {
		return err
	}
	c.drop(ctx, role.Account)
	return nil
}

func (c *cached) UpdateRole(ctx context.Context, role *model.Role) error {
	return c.inner.UpdateRole(ctx, role)
}

// EnsureIndexes 直通内层
func (c *cached) EnsureIndexes(ctx context.Context) error { return c.inner.EnsureIndexes(ctx) }

// drop 失效缓存（best effort）
func (c *cached) drop(ctx context.Context, account string) {
	if err := c.kv.Del(ctx, c.key(account)); err != nil {
		cacheLog.Warnf("cache del %q: %s", account, err)
	}
}
