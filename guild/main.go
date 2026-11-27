// guild 公会服务：game 连入，承担公会数据唯一持久层（Mongo）与业务逻辑。
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/mongo"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/redis"
	"github.com/zhenhuasu/mgame/guild/handler"
	"github.com/zhenhuasu/mgame/guild/network"
	"github.com/zhenhuasu/mgame/guild/repo"
	"github.com/zhenhuasu/mgame/guild/service"
)

func main() {
	cfgPath := flag.String("config", "config/guild.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.LoadGuild(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 优雅下线：SIGINT/SIGTERM → ctx 取消 → 事件循环退出 → 注销注册
	ctx, stop := net.SignalContext()
	defer stop()

	// 初始化（事件队列 + 日志级别 + 注册中心）
	if err := net.Init(net.InitOptions{
		LogLevel: cfg.LogLevel,
		LogNames: []string{"tcp", "guild", "svc", "discover"},
		Registry: cfg.Registry,
	}); err != nil {
		log.Fatalf("init: %v", err)
	}
	defer net.CloseRegistry()

	// MongoDB：guilds/members/invites 唯一持久层，连不上直接启动失败
	mcli, err := mongo.Open(ctx, cfg.Mongo)
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = mcli.Close(dctx)
	}()

	// Redis：全局 guildId 自增，连不上直接启动失败
	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	r := repo.NewMongo(mcli.Database())
	cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
	if err := r.EnsureIndexes(cctx); err != nil {
		ccancel()
		log.Fatalf("ensure indexes: %v", err)
	}
	ccancel()

	mgr := service.NewGuildManager(r, service.NewGuildIDGen(rdb, ""), nil,
		cfg.Guild.MaxMembers, cfg.Guild.NameMaxLen, cfg.Guild.NoticeMaxLen)
	h := handler.New(mgr, cfg.GuildSecret)
	mgr.SetNotifier(h)

	// 监听 game 连入（内部阻塞在事件循环，ctx 取消后返回）
	if err := network.Listen(ctx, cfg, h); err != nil {
		log.Fatalf("guild: %v", err)
	}
}
