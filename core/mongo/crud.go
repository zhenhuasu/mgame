package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Get 按 filter 读取单条并解码到 T；不存在返回 (zero, false, nil)
func Get[T any](ctx context.Context, coll *mongo.Collection, filter any, opts ...options.Lister[options.FindOneOptions]) (T, bool, error) {
	var out T
	err := coll.FindOne(ctx, filter, opts...).Decode(&out)
	if err == nil {
		return out, true, nil
	}
	if err == mongo.ErrNoDocuments {
		var zero T
		return zero, false, nil
	}
	var zero T
	return zero, false, fmt.Errorf("mongo %s: get: %w", coll.Name(), err)
}

// Find 按 filter 读取多条并解码为 []T（无结果返回空切片，不返回 nil）
func Find[T any](ctx context.Context, coll *mongo.Collection, filter any, opts ...options.Lister[options.FindOptions]) ([]T, error) {
	cur, err := coll.Find(ctx, filter, opts...)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: find: %w", coll.Name(), err)
	}
	defer cur.Close(ctx)

	var out []T
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("mongo %s: find decode: %w", coll.Name(), err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// Insert 插入单条文档
func Insert[T any](ctx context.Context, coll *mongo.Collection, doc T) error {
	if _, err := coll.InsertOne(ctx, doc); err != nil {
		return fmt.Errorf("mongo %s: insert: %w", coll.Name(), err)
	}
	return nil
}

// UpdateOne 更新单条；需要 upsert 用 Upsert
func UpdateOne(ctx context.Context, coll *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	res, err := coll.UpdateOne(ctx, filter, update, opts...)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: update: %w", coll.Name(), err)
	}
	return res, nil
}

// UpdateMany 批量更新
func UpdateMany(ctx context.Context, coll *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateManyOptions]) (*mongo.UpdateResult, error) {
	res, err := coll.UpdateMany(ctx, filter, update, opts...)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: update many: %w", coll.Name(), err)
	}
	return res, nil
}

// Upsert 按 filter upsert 单条
func Upsert(ctx context.Context, coll *mongo.Collection, filter, update any) (*mongo.UpdateResult, error) {
	return UpdateOne(ctx, coll, filter, update, options.UpdateOne().SetUpsert(true))
}

// DeleteOne 删除单条，返回删除数量
func DeleteOne(ctx context.Context, coll *mongo.Collection, filter any) (int64, error) {
	res, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("mongo %s: delete: %w", coll.Name(), err)
	}
	return res.DeletedCount, nil
}

// DeleteMany 批量删除，返回删除数量
func DeleteMany(ctx context.Context, coll *mongo.Collection, filter any) (int64, error) {
	res, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("mongo %s: delete many: %w", coll.Name(), err)
	}
	return res.DeletedCount, nil
}

// Count 统计匹配文档数
func Count(ctx context.Context, coll *mongo.Collection, filter any) (int64, error) {
	n, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("mongo %s: count: %w", coll.Name(), err)
	}
	return n, nil
}

// Aggregate 聚合并解码为 []T
func Aggregate[T any](ctx context.Context, coll *mongo.Collection, pipeline any, opts ...options.Lister[options.AggregateOptions]) ([]T, error) {
	cur, err := coll.Aggregate(ctx, pipeline, opts...)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: aggregate: %w", coll.Name(), err)
	}
	defer cur.Close(ctx)

	var out []T
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("mongo %s: aggregate decode: %w", coll.Name(), err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}
