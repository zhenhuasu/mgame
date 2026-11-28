package main

import (
	"context"
	"time"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/discover"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"
	"github.com/zhenhuasu/mgame/gate/backend"
	"github.com/zhenhuasu/mgame/gate/frontend"
	"github.com/zhenhuasu/mgame/gate/rankclient"
	"github.com/zhenhuasu/mgame/gate/session"
)

// gateLog 本包日志（名字含 "gate" 子串，被 LogNames "gate" 覆盖）
var gateLog = golog.New("gate")

// run 装配 gate：鉴权注入 → 登录态 → 后端连接池 → 前端监听 → 注册自身 → 事件循环。
// 阻塞至 ctx 取消（SIGINT/SIGTERM 优雅下线）。
func run(ctx context.Context, cfg *config.Gate) error {
	// 鉴权：前端 token 校验器 + 后端内网共享密钥
	frontend.Configure(cfg.Auth)
	backend.SetSecret(cfg.GateSecret)
	rankclient.SetSecret(cfg.RankSecret)

	// 客户端登录态（frontend 维护，backend 回投时查询）
	mgr := session.New(time.Duration(cfg.Auth.TokenTTL) * time.Second)

	// 后端连接池：registry 启用时按注册中心发现 game，否则静态 GameServers
	var dis discover.Discovery = discover.Static(cfg.GameServers)
	if reg := net.Registry(); reg != nil {
		dis = discover.NewRegistry(reg, registry.ServiceGame, dis)
	}
	pool := backend.New(cfg.GateID, dis, mgr)
	pool.Start(ctx)

	// rank 连接池：registry 启用时按注册中心发现 rank，否则静态 RankServers
	var rdis discover.Discovery = discover.Static(cfg.RankServers)
	if reg := net.Registry(); reg != nil {
		rdis = discover.NewRegistry(reg, registry.ServiceRank, rdis)
	}
	rpool := rankclient.New(cfg.GateID, rdis)
	rpool.Start(ctx)

	// 前端：注册客户端处理函数并起 ws 监听
	srv := frontend.New(cfg, mgr, pool, rpool)
	if err := srv.Start(); err != nil {
		return err
	}

	// 启动后注册自身（未启用注册中心时为 no-op；失败不致命）
	r, err := net.RegisterSelf(ctx, registry.Instance{
		Service: registry.ServiceGate,
		ID:      cfg.GateID,
		Addr:    cfg.Advertise,
		Meta:    map[string]string{"protocol": cfg.Protocol},
	})
	if err != nil {
		gateLog.Warnf("register gate self failed: %v", err)
	} else {
		defer r.Close()
	}

	gateLog.Infof("gate listening %s (protocol=%s) gateId=%s games=%v", cfg.Addr(), cfg.Protocol, cfg.GateID, cfg.GameServers)

	// 事件循环：ctx 取消后返回，随后 defer 注销注册
	net.RunContext(ctx)
	return nil
}
