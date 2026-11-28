package main

import (
	"flag"
	"log"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/redis"
	"github.com/zhenhuasu/mgame/rank/handler"
	"github.com/zhenhuasu/mgame/rank/network"
	"github.com/zhenhuasu/mgame/rank/repo"
	"github.com/zhenhuasu/mgame/rank/service"
)

func main() {
	cfgPath := flag.String("config", "config/rank.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.LoadRank(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := net.SignalContext()
	defer stop()

	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		LogNames: []string{"tcp", "rank", "svc", "discover"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	mgr := service.NewRankManager(repo.NewRedis(rdb), cfg.RankOptions.DefaultLimit, cfg.RankOptions.MaxLimit)
	h := handler.New(mgr, cfg.RankSecret)
	mgr.SetNotifier(h)

	if err := network.Listen(ctx, cfg, h); err != nil {
		log.Fatalf("rank: %v", err)
	}
}
