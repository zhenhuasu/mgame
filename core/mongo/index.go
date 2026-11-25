package mongo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Ensure 创建索引（已存在则无操作）
func Ensure(ctx context.Context, coll *mongo.Collection, models ...mongo.IndexModel) error {
	if len(models) == 0 {
		return nil
	}
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("mongo %s: ensure indexes: %w", coll.Name(), err)
	}
	return nil
}

// DropIfExists 删除索引；索引不存在视为成功（便于索引演进幂等）
func DropIfExists(ctx context.Context, coll *mongo.Collection, name string) error {
	if err := coll.Indexes().DropOne(ctx, name); err != nil && !isIndexNotFound(err) {
		return fmt.Errorf("mongo %s: drop index %s: %w", coll.Name(), name, err)
	}
	return nil
}

// IsDuplicate 判断是否唯一索引冲突
func IsDuplicate(err error) bool { return mongo.IsDuplicateKeyError(err) }

// DuplicateKeyValue 提取唯一索引冲突涉及的键值（keyValue 字段）；
// 非重键错误返回 (nil, false)
func DuplicateKeyValue(err error) (bson.Raw, bool) {
	var we mongo.WriteException
	if !errors.As(err, &we) {
		return nil, false
	}
	for _, e := range we.WriteErrors {
		if e.Code == 11000 && len(e.Raw) > 0 {
			var doc struct {
				KeyValue bson.Raw `bson:"keyValue"`
			}
			if derr := bson.Unmarshal(e.Raw, &doc); derr == nil && len(doc.KeyValue) > 0 {
				return doc.KeyValue, true
			}
		}
	}
	return nil, false
}

// isIndexNotFound 判断 DropOne 目标索引不存在（CommandError code 27 IndexNotFound）
func isIndexNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ce mongo.CommandError
	if errors.As(err, &ce) && ce.Code == 27 {
		return true
	}
	return strings.Contains(err.Error(), "index not found")
}
