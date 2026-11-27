package repo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	dbmongo "github.com/zhenhuasu/mgame/core/mongo"
	"github.com/zhenhuasu/mgame/guild/model"
)

const (
	colGuilds  = "guilds"
	colMembers = "guild_members"
	colInvites = "guild_invites"

	listLimit = 100
)

type guildDoc struct {
	GuildID      int64     `bson:"guild_id"`
	Name         string    `bson:"name"`
	LeaderRoleID int64     `bson:"leader_role_id"`
	Notice       string    `bson:"notice"`
	Level        int32     `bson:"level"`
	JoinMode     int32     `bson:"join_mode"`
	MemberCount  int32     `bson:"member_count"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
}

type memberDoc struct {
	GuildID   int64     `bson:"guild_id"`
	RoleID    int64     `bson:"role_id"`
	RoleName  string    `bson:"role_name"`
	RoleLevel int32     `bson:"role_level"`
	Position  int32     `bson:"position"`
	JoinedAt  time.Time `bson:"joined_at"`
}

type inviteDoc struct {
	GuildID       int64     `bson:"guild_id"`
	RoleID        int64     `bson:"role_id"`
	InvitedBy     int64     `bson:"invited_by"`
	InvitedByName string    `bson:"invited_by_name"`
	CreatedAt     time.Time `bson:"created_at"`
}

// mongoRepo Mongo 实现
type mongoRepo struct {
	guilds  *mongo.Collection
	members *mongo.Collection
	invites *mongo.Collection
}

// NewMongo 创建公会仓储
func NewMongo(db *mongo.Database) GuildRepo {
	return &mongoRepo{
		guilds:  db.Collection(colGuilds),
		members: db.Collection(colMembers),
		invites: db.Collection(colInvites),
	}
}

// EnsureIndexes 唯一索引：guilds(guild_id,name) / members.role_id、(guild_id,role_id) / invites(guild_id,role_id)
func (r *mongoRepo) EnsureIndexes(ctx context.Context) error {
	if err := dbmongo.Ensure(ctx, r.guilds,
		mongo.IndexModel{
			Keys:    bson.D{{Key: "guild_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("guilds_guild_id_uq"),
		},
		mongo.IndexModel{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("guilds_name_uq"),
		},
	); err != nil {
		return err
	}
	if err := dbmongo.Ensure(ctx, r.members,
		mongo.IndexModel{
			Keys:    bson.D{{Key: "role_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("members_role_id_uq"),
		},
		mongo.IndexModel{
			Keys:    bson.D{{Key: "guild_id", Value: 1}, {Key: "role_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("members_guild_role_uq"),
		},
		mongo.IndexModel{
			Keys:    bson.D{{Key: "guild_id", Value: 1}},
			Options: options.Index().SetName("members_guild_idx"),
		},
	); err != nil {
		return err
	}
	return dbmongo.Ensure(ctx, r.invites,
		mongo.IndexModel{
			Keys:    bson.D{{Key: "guild_id", Value: 1}, {Key: "role_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("invites_guild_role_uq"),
		},
		mongo.IndexModel{
			Keys:    bson.D{{Key: "role_id", Value: 1}},
			Options: options.Index().SetName("invites_role_idx"),
		},
	)
}

// Create 插入公会并加入会长；重名映射 ErrGuildNameExists；成员写入失败回滚公会
func (r *mongoRepo) Create(ctx context.Context, g *model.Guild, leaderName string, leaderLevel int32, maxMembers int) error {
	now := time.Now()
	err := dbmongo.Insert(ctx, r.guilds, guildDoc{
		GuildID:      g.ID,
		Name:         g.Name,
		LeaderRoleID: g.LeaderRoleID,
		Level:        g.Level,
		JoinMode:     g.JoinMode,
		MemberCount:  0,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		if dbmongo.IsDuplicate(err) {
			n, cerr := dbmongo.Count(ctx, r.guilds, bson.M{"name": g.Name})
			if cerr == nil && n > 0 {
				return ErrGuildNameExists
			}
		}
		return err
	}
	if err := r.AddMember(ctx, g.ID, g.LeaderRoleID, leaderName, leaderLevel, model.PositionLeader, maxMembers); err != nil {
		_, _ = dbmongo.DeleteOne(ctx, r.guilds, bson.M{"guild_id": g.ID})
		return err
	}
	return nil
}

// Get 按 guild_id 读取
func (r *mongoRepo) Get(ctx context.Context, guildID int64) (*model.Guild, bool, error) {
	doc, ok, err := dbmongo.Get[guildDoc](ctx, r.guilds, bson.M{"guild_id": guildID})
	if err != nil || !ok {
		return nil, ok, err
	}
	g := toGuild(doc)
	return &g, true, nil
}

// List 公会列表
func (r *mongoRepo) List(ctx context.Context, limit int64) ([]model.Guild, error) {
	if limit <= 0 {
		limit = listLimit
	}
	docs, err := dbmongo.Find[guildDoc](ctx, r.guilds, bson.M{},
		options.Find().SetSort(bson.D{{Key: "guild_id", Value: 1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	out := make([]model.Guild, 0, len(docs))
	for _, d := range docs {
		out = append(out, toGuild(d))
	}
	return out, nil
}

// SetNotice 修改公告
func (r *mongoRepo) SetNotice(ctx context.Context, guildID int64, notice string) error {
	return r.setGuild(ctx, guildID, bson.M{"notice": notice})
}

// SetJoinMode 修改加入方式
func (r *mongoRepo) SetJoinMode(ctx context.Context, guildID int64, mode int32) error {
	return r.setGuild(ctx, guildID, bson.M{"join_mode": mode})
}

// SetLeader 修改会长
func (r *mongoRepo) SetLeader(ctx context.Context, guildID int64, roleID int64) error {
	return r.setGuild(ctx, guildID, bson.M{"leader_role_id": roleID})
}

func (r *mongoRepo) setGuild(ctx context.Context, guildID int64, set bson.M) error {
	set["updated_at"] = time.Now()
	res, err := dbmongo.UpdateOne(ctx, r.guilds, bson.M{"guild_id": guildID}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrGuildNotFound
	}
	return nil
}

// Delete 删除公会文档
func (r *mongoRepo) Delete(ctx context.Context, guildID int64) error {
	_, err := dbmongo.DeleteOne(ctx, r.guilds, bson.M{"guild_id": guildID})
	return err
}

// AddMember 插入成员 + 条件自增 member_count（原子防超员）
func (r *mongoRepo) AddMember(ctx context.Context, guildID, roleID int64, name string, level, position int32, maxMembers int) error {
	_, err := r.members.InsertOne(ctx, memberDoc{
		GuildID:   guildID,
		RoleID:    roleID,
		RoleName:  name,
		RoleLevel: level,
		Position:  position,
		JoinedAt:  time.Now(),
	})
	if err != nil {
		if dbmongo.IsDuplicate(err) {
			return ErrAlreadyInGuild
		}
		return fmt.Errorf("mongo %s: insert: %w", colMembers, err)
	}

	res, err := dbmongo.UpdateOne(ctx, r.guilds,
		bson.M{"guild_id": guildID, "member_count": bson.M{"$lt": maxMembers}},
		bson.M{"$inc": bson.M{"member_count": 1}, "$set": bson.M{"updated_at": time.Now()}})
	if err != nil {
		_, _ = dbmongo.DeleteOne(ctx, r.members, bson.M{"guild_id": guildID, "role_id": roleID})
		return err
	}
	if res.MatchedCount == 0 {
		_, _ = dbmongo.DeleteOne(ctx, r.members, bson.M{"guild_id": guildID, "role_id": roleID})
		if _, ok, gerr := r.Get(ctx, guildID); gerr != nil {
			return gerr
		} else if !ok {
			return ErrGuildNotFound
		}
		return ErrGuildFull
	}
	return nil
}

// RemoveMember 移除成员并自减计数
func (r *mongoRepo) RemoveMember(ctx context.Context, guildID, roleID int64) (bool, error) {
	n, err := dbmongo.DeleteOne(ctx, r.members, bson.M{"guild_id": guildID, "role_id": roleID})
	if err != nil || n == 0 {
		return false, err
	}
	if _, err := dbmongo.UpdateOne(ctx, r.guilds,
		bson.M{"guild_id": guildID, "member_count": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"member_count": -1}, "$set": bson.M{"updated_at": time.Now()}}); err != nil {
		return true, err
	}
	return true, nil
}

// DeleteMembers 删除公会全部成员
func (r *mongoRepo) DeleteMembers(ctx context.Context, guildID int64) (int64, error) {
	return dbmongo.DeleteMany(ctx, r.members, bson.M{"guild_id": guildID})
}

// GetMember 按 role_id 查成员
func (r *mongoRepo) GetMember(ctx context.Context, roleID int64) (*model.GuildMember, bool, error) {
	doc, ok, err := dbmongo.Get[memberDoc](ctx, r.members, bson.M{"role_id": roleID})
	if err != nil || !ok {
		return nil, ok, err
	}
	m := toMember(doc)
	return &m, true, nil
}

// GetGuildMember 查公会内成员
func (r *mongoRepo) GetGuildMember(ctx context.Context, guildID, roleID int64) (*model.GuildMember, bool, error) {
	doc, ok, err := dbmongo.Get[memberDoc](ctx, r.members, bson.M{"guild_id": guildID, "role_id": roleID})
	if err != nil || !ok {
		return nil, ok, err
	}
	m := toMember(doc)
	return &m, true, nil
}

// ListMembers 公会成员列表（按 joined_at 升序）
func (r *mongoRepo) ListMembers(ctx context.Context, guildID int64) ([]model.GuildMember, error) {
	docs, err := dbmongo.Find[memberDoc](ctx, r.members, bson.M{"guild_id": guildID},
		options.Find().SetSort(bson.D{{Key: "joined_at", Value: 1}, {Key: "role_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := make([]model.GuildMember, 0, len(docs))
	for _, d := range docs {
		out = append(out, toMember(d))
	}
	return out, nil
}

// SetMemberPosition 修改成员职位
func (r *mongoRepo) SetMemberPosition(ctx context.Context, guildID, roleID int64, position int32) error {
	res, err := dbmongo.UpdateOne(ctx, r.members,
		bson.M{"guild_id": guildID, "role_id": roleID},
		bson.M{"$set": bson.M{"position": position}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotInGuild
	}
	return nil
}

// UpsertInvite 写入邀请（已存在则忽略）
func (r *mongoRepo) UpsertInvite(ctx context.Context, inv *model.GuildInvite) error {
	now := time.Now()
	_, err := dbmongo.Upsert(ctx, r.invites,
		bson.M{"guild_id": inv.GuildID, "role_id": inv.RoleID},
		bson.M{
			"$set":         bson.M{"invited_by": inv.InvitedBy, "invited_by_name": inv.InvitedByName},
			"$setOnInsert": bson.M{"created_at": now},
		})
	if err != nil && !dbmongo.IsDuplicate(err) {
		return err
	}
	return nil
}

// GetInvite 读取邀请
func (r *mongoRepo) GetInvite(ctx context.Context, guildID, roleID int64) (*model.GuildInvite, bool, error) {
	doc, ok, err := dbmongo.Get[inviteDoc](ctx, r.invites, bson.M{"guild_id": guildID, "role_id": roleID})
	if err != nil || !ok {
		return nil, ok, err
	}
	inv := toInvite(doc)
	return &inv, true, nil
}

// ListInvites 角色的待处理邀请
func (r *mongoRepo) ListInvites(ctx context.Context, roleID int64) ([]model.GuildInvite, error) {
	docs, err := dbmongo.Find[inviteDoc](ctx, r.invites, bson.M{"role_id": roleID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	out := make([]model.GuildInvite, 0, len(docs))
	for _, d := range docs {
		out = append(out, toInvite(d))
	}
	return out, nil
}

// DeleteInvite 删除单条邀请
func (r *mongoRepo) DeleteInvite(ctx context.Context, guildID, roleID int64) (bool, error) {
	n, err := dbmongo.DeleteOne(ctx, r.invites, bson.M{"guild_id": guildID, "role_id": roleID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteInvitesByRole 删除角色全部邀请
func (r *mongoRepo) DeleteInvitesByRole(ctx context.Context, roleID int64) (int64, error) {
	return dbmongo.DeleteMany(ctx, r.invites, bson.M{"role_id": roleID})
}

// DeleteInvitesByGuild 删除公会全部邀请
func (r *mongoRepo) DeleteInvitesByGuild(ctx context.Context, guildID int64) (int64, error) {
	return dbmongo.DeleteMany(ctx, r.invites, bson.M{"guild_id": guildID})
}

func toGuild(d guildDoc) model.Guild {
	return model.Guild{
		ID: d.GuildID, Name: d.Name, LeaderRoleID: d.LeaderRoleID,
		Notice: d.Notice, Level: d.Level, JoinMode: d.JoinMode,
		MemberCount: d.MemberCount, CreatedAt: d.CreatedAt,
	}
}

func toMember(d memberDoc) model.GuildMember {
	return model.GuildMember{
		GuildID: d.GuildID, RoleID: d.RoleID, RoleName: d.RoleName,
		RoleLevel: d.RoleLevel, Position: d.Position, JoinedAt: d.JoinedAt,
	}
}

func toInvite(d inviteDoc) model.GuildInvite {
	return model.GuildInvite{
		GuildID: d.GuildID, RoleID: d.RoleID, InvitedBy: d.InvitedBy,
		InvitedByName: d.InvitedByName, CreatedAt: d.CreatedAt,
	}
}
