package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/zhenhuasu/mgame/game/model"
)

const (
	colUsers = "users"
	colRoles = "roles"
)

// userDoc users 集合文档：只存账号信息，不存角色（角色归属由 roles.account 反查）
type userDoc struct {
	Account   string    `bson:"account"`
	UID       int64     `bson:"uid"`
	IP        string    `bson:"ip"`
	Version   string    `bson:"version"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// roleDoc roles 集合文档（role_id 全局唯一，name 全局唯一）
type roleDoc struct {
	RoleID    int64     `bson:"role_id"`
	Account   string    `bson:"account"`
	Name      string    `bson:"name"`
	Level     int32     `bson:"level"`
	CreatedAt time.Time `bson:"created_at"`
}

// mongoRepo Mongo 实现
type mongoRepo struct {
	users *mongo.Collection
	roles *mongo.Collection
}

// NewMongo 创建 Mongo 仓储；db 为业务数据库
func NewMongo(db *mongo.Database) UserRepo {
	return &mongoRepo{users: db.Collection(colUsers), roles: db.Collection(colRoles)}
}

// EnsureIndexes 唯一索引：users.account / roles.role_id / roles.name（角色名全局唯一）
func (r *mongoRepo) EnsureIndexes(ctx context.Context) error {
	// 清理旧的同账号唯一索引（不存在时忽略）
	if err := r.roles.Indexes().DropOne(ctx, "roles_account_name_uq"); err != nil && !isIndexNotFound(err) {
		return fmt.Errorf("drop legacy index roles.account+name: %w", err)
	}
	if _, err := r.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "account", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("users_account_uq"),
	}); err != nil {
		return fmt.Errorf("index users.account: %w", err)
	}
	if _, err := r.roles.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "role_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("roles_role_id_uq"),
		},
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("roles_name_uq"),
		},
	}); err != nil {
		return fmt.Errorf("index roles: %w", err)
	}
	return nil
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

// Login 幂等 upsert 账号文档并刷新登录信息，再回读（RoleIDs 由 roles 派生）
func (r *mongoRepo) Login(ctx context.Context, account string, uid int64, ip, version string) (*model.User, error) {
	now := time.Now()
	set := bson.M{"uid": uid, "updated_at": now}
	// 最近登录信息：非空才覆盖
	if ip != "" {
		set["ip"] = ip
	}
	if version != "" {
		set["version"] = version
	}
	if _, err := r.users.UpdateOne(ctx,
		bson.M{"account": account},
		bson.M{"$set": set, "$setOnInsert": bson.M{"created_at": now}},
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return nil, fmt.Errorf("upsert user %q: %w", account, err)
	}
	// 读自己（mongo 实现的 Load，不经缓存装饰器）
	u, ok, err := r.Load(ctx, account)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("user %q missing after upsert", account)
	}
	return u, nil
}

// Load 读 user 文档，并按 account 查 roles 派生 RoleIDs（升序）
func (r *mongoRepo) Load(ctx context.Context, account string) (*model.User, bool, error) {
	var doc userDoc
	if err := r.users.FindOne(ctx, bson.M{"account": account}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("find user %q: %w", account, err)
	}

	cur, err := r.roles.Find(ctx,
		bson.M{"account": account},
		options.Find().
			SetProjection(bson.M{"role_id": 1}).
			SetSort(bson.D{{Key: "role_id", Value: 1}}))
	if err != nil {
		return nil, false, fmt.Errorf("find role ids %q: %w", account, err)
	}
	var ids []struct {
		RoleID int64 `bson:"role_id"`
	}
	if err := cur.All(ctx, &ids); err != nil {
		return nil, false, fmt.Errorf("decode role ids %q: %w", account, err)
	}

	u := &model.User{
		Account: doc.Account,
		UID:     doc.UID,
		IP:      doc.IP,
		Version: doc.Version,
		RoleIDs: make([]int64, 0, len(ids)),
	}
	for _, id := range ids {
		u.RoleIDs = append(u.RoleIDs, id.RoleID)
	}
	return u, true, nil
}

// LoadRoles 读取账号下角色基本信息（登录回包用）
func (r *mongoRepo) LoadRoles(ctx context.Context, account string) ([]model.Role, error) {
	cur, err := r.roles.Find(ctx, bson.M{"account": account},
		options.Find().SetSort(bson.D{{Key: "role_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find roles %q: %w", account, err)
	}
	var docs []roleDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode roles %q: %w", account, err)
	}
	out := make([]model.Role, 0, len(docs))
	for _, rd := range docs {
		out = append(out, model.Role{ID: rd.RoleID, Account: rd.Account, Name: rd.Name, Level: rd.Level})
	}
	return out, nil
}

// LoadRole 读取单个角色完整数据（按 account 限定归属）
func (r *mongoRepo) LoadRole(ctx context.Context, account string, roleID int64) (*model.Role, bool, error) {
	var doc roleDoc
	if err := r.roles.FindOne(ctx, bson.M{"account": account, "role_id": roleID}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("find role %q#%d: %w", account, roleID, err)
	}
	return &model.Role{ID: doc.RoleID, Account: doc.Account, Name: doc.Name, Level: doc.Level}, true, nil
}

// CreateRole 插入角色（不写 users，角色归属由 roles.account 表达）；
// 唯一索引冲突映射为 ErrRoleNameExists
func (r *mongoRepo) CreateRole(ctx context.Context, role *model.Role) error {
	_, err := r.roles.InsertOne(ctx, roleDoc{
		RoleID:    role.ID,
		Account:   role.Account,
		Name:      role.Name,
		Level:     role.Level,
		CreatedAt: time.Now(),
	})
	if err == nil {
		return nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("insert role %q#%d: %w", role.Account, role.ID, err)
	}
	// 区分 name 重名（全局）与 role_id 撞号（后者为内部 bug）
	n, cerr := r.roles.CountDocuments(ctx, bson.M{"name": role.Name})
	if cerr == nil && n > 0 {
		return ErrRoleNameExists
	}
	return fmt.Errorf("insert role %q#%d: duplicate key: %w", role.Account, role.ID, err)
}
