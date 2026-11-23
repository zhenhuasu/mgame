package main

import (
	"flag"
	"log"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/game/network"
)

func main() {
	cfgPath := flag.String("config", "config/game.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.LoadGame(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 优雅下线：SIGINT/SIGTERM → ctx 取消 → 事件循环退出 → 注销注册
	ctx, stop := net.SignalContext()
	defer stop()

	// 第1步：初始化（事件队列 + 日志级别 + 注册中心）
	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		// 正则子串匹配："tcp"→tcppeer，"game"→game，"svc"→svcproc
		LogNames: []string{"tcp", "game", "svc"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	// 第2步：监听 gate 连入（内部阻塞在事件循环，ctx 取消后返回）
	if err := network.Listen(ctx, cfg); err != nil {
		log.Fatalf("game: %v", err)
	}
}
