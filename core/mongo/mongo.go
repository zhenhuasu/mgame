// Package mongo 封装 MongoDB 连接与通用 CRUD/索引助手（各服务共用）。
// 文档结构、集合名、索引定义与业务错误语义由各业务仓储层负责。
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

// Client Mongo 客户端（连接 + 已选数据库），退出时须调用 Close
type Client struct {
	cli *mongo.Client
	db  *mongo.Database
}

// Open 建连、Ping 校验（3s 超时）并选定业务数据库；失败返回 error，
// 调用方按需 fail-fast。调用方在退出时对返回的 client 调 Close。
func Open(ctx context.Context, cfg config.Mongo) (*Client, error) {
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
	return &Client{cli: cli, db: cli.Database(cfg.Database)}, nil
}

// Database 取业务数据库
func (c *Client) Database() *mongo.Database { return c.db }

// Collection 取业务集合
func (c *Client) Collection(name string) *mongo.Collection { return c.db.Collection(name) }

// Raw 返回原始客户端（逃生口）
func (c *Client) Raw() *mongo.Client { return c.cli }

// Close 释放连接
func (c *Client) Close(ctx context.Context) error { return c.cli.Disconnect(ctx) }
