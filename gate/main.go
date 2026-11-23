package main

import (
	"flag"
	"log"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/gate/network"
)

func main() {
	cfgPath := flag.String("config", "config/gate.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.LoadGate(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 优雅下线：SIGINT/SIGTERM → ctx 取消 → 事件循环退出 → 注销注册
	ctx, stop := net.SignalContext()
	defer stop()

	// 第1步：初始化（事件队列 + 日志级别 + 注册中心）
	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		// 正则子串匹配："gorillaws"→gorillaws+gorillawsproc，"gate"→gate+gateproc，"tcp"→tcppeer，"svc"→svcproc
		LogNames: []string{"gorillaws", "tcp", "gate", "svc"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	// 鉴权配置：注入 token 校验器、会话 TTL、内网共享密钥
	network.ConfigureAuth(cfg)

	// 第2步：连接 game 服务（registry 启用时按注册中心发现，否则静态 GameServers）
	network.Connect(ctx, cfg)

	// 第3步：监听客户端 ws（内部阻塞在事件循环，ctx 取消后返回）
	if err := network.Listen(ctx, cfg); err != nil {
		log.Fatalf("gate: %v", err)
	}
}
