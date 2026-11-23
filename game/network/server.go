// Package network 封装 game 服务的 cellnet 装配细节，
// 对外只暴露 Listen，main 不直接接触 cellnet。
package network

import (
	"context"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"
)

var log = golog.New("game")

// Listen 监听 gate 连入（第2步）：注册处理函数、建对内 acceptor，
// 启动后经 net.RegisterSelf 注册自身（未启用注册中心时 no-op），
// 内部阻塞在事件循环直到 ctx 取消（优雅下线）或退出。
func Listen(ctx context.Context, cfg *config.Game) error {
	// 内网握手共享密钥（空 = 不校验，仅联调）
	gateSecret = cfg.GateSecret

	// 注册对内处理函数（须在起循环前）
	registerHandlers()

	net.Listen(net.Options{
		Name: "game",
		Addr: cfg.Listen,
	})

	// 启动后注册自身（失败不致命：仅影响注册中心可见性）
	r, err := net.RegisterSelf(ctx, registry.Instance{
		Service: registry.ServiceGame,
		ID:      cfg.GameID,
		Addr:    cfg.Advertise,
		Meta:    map[string]string{"listen": cfg.Listen},
	})
	if err != nil {
		log.Warnf("register game self failed: %v", err)
	} else {
		defer r.Close()
	}

	log.Infof("game listening %s id=%s advertise=%s", cfg.Listen, cfg.GameID, cfg.Advertise)

	net.RunContext(ctx)
	return nil
}
