package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/discover"
	"github.com/zhenhuasu/mgame/core/mongo"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/redis"
	"github.com/zhenhuasu/mgame/core/registry"
	"github.com/zhenhuasu/mgame/game/guildclient"
	"github.com/zhenhuasu/mgame/game/handler"
	"github.com/zhenhuasu/mgame/game/network"
	"github.com/zhenhuasu/mgame/game/rankclient"
	"github.com/zhenhuasu/mgame/game/repo"
	"github.com/zhenhuasu/mgame/game/service"
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

	// 初始化（事件队列 + 日志级别 + 注册中心）
	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		// 正则子串匹配："tcp"→tcppeer，"game"→game，"svc"→svcproc
		LogNames: []string{"tcp", "game", "svc", "discover"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	// MongoDB：user/role 唯一持久层，连不上直接启动失败
	mcli, err := mongo.Open(ctx, cfg.Mongo)
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = mcli.Close(dctx)
	}()

	// Redis：读缓存 + 全局 roleId 自增，连不上直接启动失败
	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	// 仓储：Mongo 持久 + Redis 读缓存装饰
	r := repo.NewMongo(mcli.Database())
	cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
	if err := r.EnsureIndexes(cctx); err != nil {
		ccancel()
		log.Fatalf("ensure indexes: %v", err)
	}
	ccancel()
	stored := repo.NewCached(r, rdb, cfg.User.Prefix, time.Duration(cfg.User.TTL)*time.Second)

	mgr := service.NewUserManager(stored, service.NewRoleIDGen(rdb, ""), cfg.User.AutoCreateRole,
		service.RoleRules{
			ExpBase:       cfg.Role.ExpBase,
			PowerPerLevel: cfg.Role.PowerPerLevel,
			MaxLevel:      cfg.Role.MaxLevel,
		})

	// guild 连接池：registry 启用时按注册中心发现 guild，否则静态 GuildServers
	var gdis discover.Discovery = discover.Static(cfg.GuildServers)
	if reg := net.Registry(); reg != nil {
		gdis = discover.NewRegistry(reg, registry.ServiceGuild, gdis)
	}
	guildclient.SetSecret(cfg.GuildSecret)
	gpool := guildclient.New(cfg.GameID, gdis)

	var rdis discover.Discovery = discover.Static(cfg.RankServers)
	if reg := net.Registry(); reg != nil {
		rdis = discover.NewRegistry(reg, registry.ServiceRank, rdis)
	}
	rankclient.SetSecret(cfg.RankSecret)
	rpool := rankclient.New(cfg.GameID, rdis)

	h := handler.New(mgr, cfg.GateSecret, gpool, rpool)
	mgr.SetProgressNotifier(h)
	gpool.SetForwarder(h)
	rpool.SetForwarder(h)
	gpool.Start(ctx)
	rpool.Start(ctx)

	// 监听 gate 连入（内部阻塞在事件循环，ctx 取消后返回）
	if err := network.Listen(ctx, cfg, h); err != nil {
		log.Fatalf("game: %v", err)
	}
}
