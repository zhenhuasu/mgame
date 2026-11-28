package service

import (
	"context"
	"testing"

	"github.com/zhenhuasu/mgame/game/model"
	"github.com/zhenhuasu/mgame/game/repo"
)

type fakeUserRepo struct {
	role *model.Role
}

func (f *fakeUserRepo) Login(context.Context, string, int64, string, string) (*model.User, error) {
	return &model.User{Account: "a"}, nil
}
func (f *fakeUserRepo) Load(context.Context, string) (*model.User, bool, error) {
	return &model.User{Account: "a"}, true, nil
}
func (f *fakeUserRepo) LoadRoles(context.Context, string) ([]model.Role, error) {
	return nil, nil
}
func (f *fakeUserRepo) LoadRole(context.Context, string, int64) (*model.Role, bool, error) {
	return f.role, true, nil
}
func (f *fakeUserRepo) LoadRoleByName(context.Context, string) (*model.Role, bool, error) {
	return f.role, true, nil
}
func (f *fakeUserRepo) CreateRole(context.Context, *model.Role) error { return nil }
func (f *fakeUserRepo) UpdateRole(_ context.Context, role *model.Role) error {
	cp := *role
	f.role = &cp
	return nil
}
func (f *fakeUserRepo) EnsureIndexes(context.Context) error { return nil }

type fakeNotifier struct {
	uid      int64
	role     *model.Role
	levelUps int
	calls    int
}

func (f *fakeNotifier) RoleProgressChanged(uid int64, role *model.Role, levelUps int) {
	f.uid, f.role, f.levelUps = uid, role, levelUps
	f.calls++
}

func TestGainExpLevelsUpAndUpdatesPower(t *testing.T) {
	role := &model.Role{ID: 7, Account: "a", Name: "r", Level: 1}
	r := &fakeUserRepo{role: role}
	m := NewUserManager(r, nil, false, RoleRules{ExpBase: 100, PowerPerLevel: 100, MaxLevel: 100})
	m.users[1] = &OnlineUser{UID: 1, Account: "a", User: &model.User{Account: "a", OnlineRole: role}}
	n := &fakeNotifier{}
	m.SetProgressNotifier(n)

	got, ups, err := m.GainExp(context.Background(), 1, 250)
	if err != nil {
		t.Fatal(err)
	}
	if ups != 1 || got.Level != 2 || got.Exp != 150 || got.Power != 100 {
		t.Fatalf("got level=%d exp=%d power=%d ups=%d", got.Level, got.Exp, got.Power, ups)
	}
	if n.calls != 1 || n.levelUps != 1 || n.role != got {
		t.Fatalf("notifier calls=%d levelUps=%d", n.calls, n.levelUps)
	}
	if r.role.Level != 2 || r.role.Exp != 150 || r.role.Power != 100 {
		t.Fatalf("persisted level=%d exp=%d power=%d", r.role.Level, r.role.Exp, r.role.Power)
	}
}

func TestGainExpWithoutLevelUpDoesNotNotify(t *testing.T) {
	role := &model.Role{ID: 7, Account: "a", Name: "r", Level: 1}
	m := NewUserManager(&fakeUserRepo{role: role}, nil, false, RoleRules{ExpBase: 100, PowerPerLevel: 100, MaxLevel: 100})
	m.users[1] = &OnlineUser{UID: 1, Account: "a", User: &model.User{Account: "a", OnlineRole: role}}
	n := &fakeNotifier{}
	m.SetProgressNotifier(n)

	got, ups, err := m.GainExp(context.Background(), 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if ups != 0 || got.Level != 1 || got.Exp != 50 || got.Power != 0 {
		t.Fatalf("got level=%d exp=%d power=%d ups=%d", got.Level, got.Exp, got.Power, ups)
	}
	if n.calls != 0 {
		t.Fatalf("notifier calls=%d", n.calls)
	}
}

var _ repo.UserRepo = (*fakeUserRepo)(nil)
