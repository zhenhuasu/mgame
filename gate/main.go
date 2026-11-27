package main

import (
	"flag"
	"log"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
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

	// 初始化（事件队列 + 日志级别 + 注册中心）
	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		// 正则子串匹配："gorillaws"→gorillaws+gorillawsproc，"gate"→gate+gateproc，"tcp"→tcppeer，"svc"→svcproc
		LogNames: []string{"gorillaws", "tcp", "gate", "svc", "discover"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	// 装配并运行（内部阻塞在事件循环，ctx 取消后返回）
	if err := run(ctx, cfg); err != nil {
		log.Fatalf("gate: %v", err)
	}
}
