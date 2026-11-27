// Package network 封装 guild 服务的 cellnet 装配细节，
// 对外只暴露 Listen，main 不直接接触 cellnet。
package network

import (
	"context"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"
	"github.com/zhenhuasu/mgame/guild/handler"
)

var log = golog.New("guild.network")

// Listen 监听 game 连入：注册处理函数、建对内 acceptor，
// 启动后经 net.RegisterSelf 注册自身（未启用注册中心时 no-op），
// 内部阻塞在事件循环直到 ctx 取消（优雅下线）或退出。
func Listen(ctx context.Context, cfg *config.Guild, h *handler.Handler) error {
	// 注册对内处理函数（须在起循环前）
	h.Register()

	net.Listen(net.Options{
		Name: "guild",
		Addr: cfg.Listen,
	})

	// 启动后注册自身（失败不致命：仅影响注册中心可见性）
	r, err := net.RegisterSelf(ctx, registry.Instance{
		Service: registry.ServiceGuild,
		ID:      cfg.GuildID,
		Addr:    cfg.Advertise,
		Meta:    map[string]string{"listen": cfg.Listen},
	})
	if err != nil {
		log.Warnf("register guild self failed: %v", err)
	} else {
		defer r.Close()
	}

	log.Infof("guild listening %s id=%s advertise=%s", cfg.Listen, cfg.GuildID, cfg.Advertise)

	net.RunContext(ctx)
	return nil
}
