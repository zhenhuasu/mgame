// Package mongo 封装 MongoDB 连接（各服务共用）；
// 用户/角色等文档读写由各业务的仓储层负责。
package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/zhenhuasu/mgame/config"
)

// Connect 建连并 Ping 校验（3s 超时）；失败返回 error，调用方按需 fail-fast。
// 调用方在退出时对返回的 client 调 Disconnect。
func Connect(ctx context.Context, cfg config.Mongo) (*mongo.Client, error) {
	if !cfg.Enabled() {
		return nil, fmt.Errorf("mongo.uri required")
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cli, err := mongo.Connect(options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("mongo connect %s: %w", cfg.URI, err)
	}
	if err := cli.Ping(cctx, readpref.Primary()); err != nil {
		_ = cli.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping %s: %w", cfg.URI, err)
	}
	return cli, nil
}

// Database 取业务数据库
func Database(cli *mongo.Client, cfg config.Mongo) *mongo.Database {
	return cli.Database(cfg.Database)
}
