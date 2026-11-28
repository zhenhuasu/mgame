package network

import (
	"context"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/registry"
	"github.com/zhenhuasu/mgame/rank/handler"
)

var log = golog.New("rank.network")

func Listen(ctx context.Context, cfg *config.Rank, h *handler.Handler) error {
	h.Register()

	net.Listen(net.Options{
		Name: "rank",
		Addr: cfg.Listen,
	})

	r, err := net.RegisterSelf(ctx, registry.Instance{
		Service: registry.ServiceRank,
		ID:      cfg.RankID,
		Addr:    cfg.Advertise,
		Meta:    map[string]string{"listen": cfg.Listen},
	})
	if err != nil {
		log.Warnf("register rank self failed: %v", err)
	} else {
		defer r.Close()
	}

	log.Infof("rank listening %s id=%s advertise=%s", cfg.Listen, cfg.RankID, cfg.Advertise)

	net.RunContext(ctx)
	return nil
}
