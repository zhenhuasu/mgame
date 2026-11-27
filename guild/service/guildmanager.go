// Package service 公会业务逻辑：权限、容量、邀请与关键事件推送。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/guild/model"
	"github.com/zhenhuasu/mgame/guild/repo"
)

var log = golog.New("guild.service")

// RoleCtx 已由 game 校验的请求角色上下文（guild 不信任客户端直连）
type RoleCtx struct {
	RoleID int64
	Name   string
	Level  int32
}

// Notifier 公会推送（handler 实现：GuildInviteNtf / GuildEventNtf）
type Notifier interface {
	Invite(roleID int64, guildID, invitedBy int64, guildName, invitedByName string)
	Event(roleID int64, kind int32, guildID int64, guildName string, position int32, operatorRoleID int64)
}

// InviteView 邀请列表项（带公会名）
type InviteView struct {
	GuildID       int64
	GuildName     string
	InvitedBy     int64
	InvitedByName string
	CreatedAt     time.Time
}

// GuildManager 公会管理器
type GuildManager struct {
	repo         repo.GuildRepo
	idgen        *GuildIDGen
	maxMembers   int
	nameMaxLen   int
	noticeMaxLen int
	notifier     Notifier
}

// NewGuildManager 创建公会管理器（notifier 可后置 SetNotifier）
func NewGuildManager(r repo.GuildRepo, idgen *GuildIDGen, notifier Notifier, maxMembers, nameMaxLen, noticeMaxLen int) *GuildManager {
	return &GuildManager{
		repo:         r,
		idgen:        idgen,
		maxMembers:   maxMembers,
		nameMaxLen:   nameMaxLen,
		noticeMaxLen: noticeMaxLen,
		notifier:     notifier,
	}
}

// SetNotifier 绑定推送器（handler 构造完成后注入）
func (m *GuildManager) SetNotifier(n Notifier) { m.notifier = n }

func (m *GuildManager) notifyEvent(roleID int64, kind int32, g *model.Guild, position int32, operator int64) {
	if m.notifier != nil {
		m.notifier.Event(roleID, kind, g.ID, g.Name, position, operator)
	}
}

func (m *GuildManager) mustMember(ctx context.Context, roleID int64) (*model.GuildMember, error) {
	mb, ok, err := m.repo.GetMember(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, repo.ErrNotInGuild
	}
	return mb, nil
}

func (m *GuildManager) mustGuild(ctx context.Context, guildID int64) (*model.Guild, error) {
	g, ok, err := m.repo.Get(ctx, guildID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, repo.ErrGuildNotFound
	}
	return g, nil
}

// Create 创建公会（名称全局唯一，会长自动入会）
func (m *GuildManager) Create(ctx context.Context, rc RoleCtx, name string) (*model.Guild, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > m.nameMaxLen {
		return nil, fmt.Errorf("%w: guild name length must be 1-%d", repo.ErrInvalidArgument, m.nameMaxLen)
	}
	if _, ok, err := m.repo.GetMember(ctx, rc.RoleID); err != nil {
		return nil, err
	} else if ok {
		return nil, repo.ErrAlreadyInGuild
	}
	id, err := m.idgen.Next(ctx)
	if err != nil {
		return nil, err
	}
	g := &model.Guild{
		ID:           id,
		Name:         name,
		LeaderRoleID: rc.RoleID,
		Level:        model.DefaultLevel,
		JoinMode:     model.JoinOpen,
		MemberCount:  1,
		CreatedAt:    time.Now(),
	}
	if err := m.repo.Create(ctx, g, rc.Name, rc.Level, m.maxMembers); err != nil {
		return nil, err
	}
	log.Infof("guild created id=%d name=%q leader=%d", g.ID, g.Name, rc.RoleID)
	return g, nil
}

// Join 直接加入（受 join_mode 控制；role_id 唯一索引防重复入会）
func (m *GuildManager) Join(ctx context.Context, rc RoleCtx, guildID int64) (*model.Guild, error) {
	g, err := m.mustGuild(ctx, guildID)
	if err != nil {
		return nil, err
	}
	if g.JoinMode != model.JoinOpen {
		return nil, repo.ErrJoinClosed
	}
	if err := m.repo.AddMember(ctx, guildID, rc.RoleID, rc.Name, rc.Level, model.PositionMember, m.maxMembers); err != nil {
		return nil, err
	}
	_, _ = m.repo.DeleteInvitesByRole(ctx, rc.RoleID)
	g.MemberCount++
	log.Infof("guild join guild=%d role=%d", guildID, rc.RoleID)
	return g, nil
}

// Leave 退出公会（会长必须转让或解散）
func (m *GuildManager) Leave(ctx context.Context, rc RoleCtx) error {
	mb, err := m.mustMember(ctx, rc.RoleID)
	if err != nil {
		return err
	}
	if mb.Position == model.PositionLeader {
		return repo.ErrLeaderCannotLeave
	}
	removed, err := m.repo.RemoveMember(ctx, mb.GuildID, rc.RoleID)
	if err != nil {
		return err
	}
	if !removed {
		return repo.ErrNotInGuild
	}
	log.Infof("guild leave guild=%d role=%d", mb.GuildID, rc.RoleID)
	return nil
}

// Kick 踢人：会长可踢除自己外的任何人；副会长仅能踢普通成员
func (m *GuildManager) Kick(ctx context.Context, rc RoleCtx, target int64) error {
	op, err := m.mustMember(ctx, rc.RoleID)
	if err != nil {
		return err
	}
	if op.Position != model.PositionLeader && op.Position != model.PositionVice {
		return repo.ErrNoPermission
	}
	if target == rc.RoleID {
		return fmt.Errorf("%w: cannot kick yourself", repo.ErrInvalidArgument)
	}
	tgt, ok, err := m.repo.GetGuildMember(ctx, op.GuildID, target)
	if err != nil {
		return err
	}
	if !ok {
		return repo.ErrNotInGuild
	}
	if tgt.Position == model.PositionLeader {
		return repo.ErrNoPermission
	}
	if op.Position == model.PositionVice && tgt.Position != model.PositionMember {
		return repo.ErrNoPermission
	}
	removed, err := m.repo.RemoveMember(ctx, op.GuildID, target)
	if err != nil {
		return err
	}
	if !removed {
		return repo.ErrNotInGuild
	}
	g, err := m.mustGuild(ctx, op.GuildID)
	if err != nil {
		return err
	}
	m.notifyEvent(target, model.EventKicked, g, 0, rc.RoleID)
	log.Infof("guild kick guild=%d role=%d by=%d", op.GuildID, target, rc.RoleID)
	return nil
}

// Transfer 转让会长：旧会长降为普通成员
func (m *GuildManager) Transfer(ctx context.Context, rc RoleCtx, target int64) error {
	g, err := m.mustGuildByRole(ctx, rc.RoleID, model.PositionLeader)
	if err != nil {
		return err
	}
	if target == rc.RoleID {
		return fmt.Errorf("%w: cannot transfer to yourself", repo.ErrInvalidArgument)
	}
	if _, ok, err := m.repo.GetGuildMember(ctx, g.ID, target); err != nil {
		return err
	} else if !ok {
		return repo.ErrNotInGuild
	}
	if err := m.repo.SetLeader(ctx, g.ID, target); err != nil {
		return err
	}
	if err := m.repo.SetMemberPosition(ctx, g.ID, target, model.PositionLeader); err != nil {
		return err
	}
	if err := m.repo.SetMemberPosition(ctx, g.ID, rc.RoleID, model.PositionMember); err != nil {
		return err
	}
	m.notifyEvent(target, model.EventTransfer, g, model.PositionLeader, rc.RoleID)
	m.notifyEvent(rc.RoleID, model.EventTransfer, g, model.PositionMember, rc.RoleID)
	log.Infof("guild transfer guild=%d from=%d to=%d", g.ID, rc.RoleID, target)
	return nil
}

func (m *GuildManager) mustGuildByRole(ctx context.Context, roleID int64, want int32) (*model.Guild, error) {
	mb, err := m.mustMember(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if mb.Position != want {
		return nil, repo.ErrNoPermission
	}
	return m.mustGuild(ctx, mb.GuildID)
}

// SetNotice 修改公告（会长/副会长）
func (m *GuildManager) SetNotice(ctx context.Context, rc RoleCtx, notice string) error {
	notice = strings.TrimSpace(notice)
	if utf8.RuneCountInString(notice) > m.noticeMaxLen {
		return fmt.Errorf("%w: notice too long (max %d)", repo.ErrInvalidArgument, m.noticeMaxLen)
	}
	mb, err := m.mustMember(ctx, rc.RoleID)
	if err != nil {
		return err
	}
	if mb.Position != model.PositionLeader && mb.Position != model.PositionVice {
		return repo.ErrNoPermission
	}
	return m.repo.SetNotice(ctx, mb.GuildID, notice)
}

// SetJoinMode 设置加入方式（仅会长）
func (m *GuildManager) SetJoinMode(ctx context.Context, rc RoleCtx, mode int32) error {
	if mode != model.JoinOpen && mode != model.JoinInviteOnly {
		return fmt.Errorf("%w: bad join mode", repo.ErrInvalidArgument)
	}
	mb, err := m.mustMember(ctx, rc.RoleID)
	if err != nil {
		return err
	}
	if mb.Position != model.PositionLeader {
		return repo.ErrNoPermission
	}
	return m.repo.SetJoinMode(ctx, mb.GuildID, mode)
}

// SetPosition 任免副会长/撤销（仅会长；目标不能是会长）
func (m *GuildManager) SetPosition(ctx context.Context, rc RoleCtx, target int64, position int32) error {
	if position != model.PositionVice && position != model.PositionMember {
		return fmt.Errorf("%w: position must be vice or member", repo.ErrInvalidArgument)
	}
	g, err := m.mustGuildByRole(ctx, rc.RoleID, model.PositionLeader)
	if err != nil {
		return err
	}
	if target == rc.RoleID {
		return fmt.Errorf("%w: cannot change leader position", repo.ErrInvalidArgument)
	}
	tgt, ok, err := m.repo.GetGuildMember(ctx, g.ID, target)
	if err != nil {
		return err
	}
	if !ok {
		return repo.ErrNotInGuild
	}
	if tgt.Position == model.PositionLeader {
		return fmt.Errorf("%w: cannot change leader position", repo.ErrInvalidArgument)
	}
	if tgt.Position == position {
		return nil
	}
	if err := m.repo.SetMemberPosition(ctx, g.ID, target, position); err != nil {
		return err
	}
	m.notifyEvent(target, model.EventPosition, g, position, rc.RoleID)
	return nil
}

// Disband 解散公会（仅会长；先发事件再删数据）
func (m *GuildManager) Disband(ctx context.Context, rc RoleCtx) error {
	g, err := m.mustGuildByRole(ctx, rc.RoleID, model.PositionLeader)
	if err != nil {
		return err
	}
	members, err := m.repo.ListMembers(ctx, g.ID)
	if err != nil {
		return err
	}
	for _, mb := range members {
		m.notifyEvent(mb.RoleID, model.EventDisbanded, g, 0, rc.RoleID)
	}
	if _, err := m.repo.DeleteInvitesByGuild(ctx, g.ID); err != nil {
		return err
	}
	if _, err := m.repo.DeleteMembers(ctx, g.ID); err != nil {
		return err
	}
	if err := m.repo.Delete(ctx, g.ID); err != nil {
		return err
	}
	log.Infof("guild disbanded id=%d name=%q leader=%d", g.ID, g.Name, rc.RoleID)
	return nil
}

// Info 公会信息 + 成员列表；guildID=0 表示自己所在公会
func (m *GuildManager) Info(ctx context.Context, roleID, guildID int64) (*model.Guild, []model.GuildMember, error) {
	if guildID == 0 {
		mb, err := m.mustMember(ctx, roleID)
		if err != nil {
			return nil, nil, err
		}
		guildID = mb.GuildID
	}
	g, err := m.mustGuild(ctx, guildID)
	if err != nil {
		return nil, nil, err
	}
	members, err := m.repo.ListMembers(ctx, guildID)
	if err != nil {
		return nil, nil, err
	}
	return g, members, nil
}

// List 公会列表
func (m *GuildManager) List(ctx context.Context) ([]model.Guild, error) {
	return m.repo.List(ctx, 0)
}

// Invite 邀请入会（会长/副会长；目标需不在任何公会）
func (m *GuildManager) Invite(ctx context.Context, rc RoleCtx, target int64) error {
	if target == rc.RoleID {
		return fmt.Errorf("%w: cannot invite yourself", repo.ErrInvalidArgument)
	}
	mb, err := m.mustMember(ctx, rc.RoleID)
	if err != nil {
		return err
	}
	if mb.Position != model.PositionLeader && mb.Position != model.PositionVice {
		return repo.ErrNoPermission
	}
	g, err := m.mustGuild(ctx, mb.GuildID)
	if err != nil {
		return err
	}
	if int(g.MemberCount) >= m.maxMembers {
		return repo.ErrGuildFull
	}
	if _, ok, err := m.repo.GetMember(ctx, target); err != nil {
		return err
	} else if ok {
		return repo.ErrAlreadyInGuild
	}
	if err := m.repo.UpsertInvite(ctx, &model.GuildInvite{
		GuildID:       g.ID,
		RoleID:        target,
		InvitedBy:     rc.RoleID,
		InvitedByName: rc.Name,
	}); err != nil {
		return err
	}
	if m.notifier != nil {
		m.notifier.Invite(target, g.ID, rc.RoleID, g.Name, rc.Name)
	}
	log.Infof("guild invite guild=%d role=%d by=%d", g.ID, target, rc.RoleID)
	return nil
}

// ListInvites 角色待处理邀请
func (m *GuildManager) ListInvites(ctx context.Context, roleID int64) ([]InviteView, error) {
	invs, err := m.repo.ListInvites(ctx, roleID)
	if err != nil {
		return nil, err
	}
	out := make([]InviteView, 0, len(invs))
	for _, inv := range invs {
		g, ok, err := m.repo.Get(ctx, inv.GuildID)
		if err != nil {
			return nil, err
		}
		if !ok {
			_, _ = m.repo.DeleteInvite(ctx, inv.GuildID, inv.RoleID)
			continue
		}
		out = append(out, InviteView{
			GuildID:       inv.GuildID,
			GuildName:     g.Name,
			InvitedBy:     inv.InvitedBy,
			InvitedByName: inv.InvitedByName,
			CreatedAt:     inv.CreatedAt,
		})
	}
	return out, nil
}

// RespondInvite 响应邀请：accept 时入会并清理该角色其他邀请
func (m *GuildManager) RespondInvite(ctx context.Context, rc RoleCtx, guildID int64, accept bool) (*model.Guild, error) {
	_, ok, err := m.repo.GetInvite(ctx, guildID, rc.RoleID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, repo.ErrInviteNotFound
	}
	if !accept {
		if _, err := m.repo.DeleteInvite(ctx, guildID, rc.RoleID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	g, err := m.mustGuild(ctx, guildID)
	if err != nil {
		_, _ = m.repo.DeleteInvite(ctx, guildID, rc.RoleID)
		return nil, err
	}
	if err := m.repo.AddMember(ctx, g.ID, rc.RoleID, rc.Name, rc.Level, model.PositionMember, m.maxMembers); err != nil {
		if err == repo.ErrAlreadyInGuild {
			_, _ = m.repo.DeleteInvite(ctx, guildID, rc.RoleID)
		}
		return nil, err
	}
	_, _ = m.repo.DeleteInvite(ctx, guildID, rc.RoleID)
	_, _ = m.repo.DeleteInvitesByRole(ctx, rc.RoleID)
	g.MemberCount++
	log.Infof("guild invite accepted guild=%d role=%d", g.ID, rc.RoleID)
	return g, nil
}
