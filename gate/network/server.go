// Package network 封装 gate 服务的 cellnet 装配细节，
// 对外暴露 Connect（连 game）与 Listen（监听客户端 ws），main 不直接接触 cellnet。
package network

import (
	"context"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/peer"

	_ "github.com/davyxu/cellnet/peer/gorillaws"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"
	_ "github.com/zhenhuasu/mgame/gate/network/wproc" // 注册自定义 proc "gate.ws"
)

var log = golog.New("gate")

// Listen 监听客户端 ws 连接（第3步）。
// 装配保持 gate 独有（gorillaws Acceptor / wss 证书），事件队列与消息分发复用 net；
// 启动后向 net.Init 创建的注册中心注册自身；
// 内部阻塞在事件循环直到 ctx 取消（优雅下线）或退出。
func Listen(ctx context.Context, cfg *config.Gate) error {
	// 注册 ws 侧处理函数（含 SessionClosed 分流，须在起循环前）
	registerWsHandlers()

	// 地址需含 scheme/path；cellnet 按 path 注册 HTTP 升级路由
	p := peer.NewGenericPeer("gorillaws.Acceptor", "gate", cfg.Addr(), net.Queue())

	// wss: 提供证书后底层走 ServeTLS
	if cfg.Protocol == "wss" {
		p.(cellnet.WSAcceptor).SetHttps(cfg.CertFile, cfg.KeyFile)
	}

	// 注入会话查找，供 Manager 顶号定位旧会话与广播发送
	csa := p.(cellnet.SessionAccessor)
	sessionLookup = csa.GetSession

	net.Bind(p, "gate.ws")
	p.Start()

	// 启动后注册自身（未启用注册中心时为 no-op；失败不致命）
	r, err := net.RegisterSelf(ctx, registry.Instance{
		Service: registry.ServiceGate,
		ID:      cfg.GateID,
		Addr:    cfg.Advertise,
		Meta:    map[string]string{"protocol": cfg.Protocol},
	})
	if err != nil {
		log.Warnf("register gate self failed: %v", err)
	} else {
		defer r.Close()
	}

	log.Infof("gate listening %s (protocol=%s) gateId=%s games=%v", cfg.Addr(), cfg.Protocol, cfg.GateID, cfg.GameServers)

	net.RunContext(ctx)
	return nil
}
