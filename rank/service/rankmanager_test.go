package service

import (
	"context"
	"testing"

	"github.com/zhenhuasu/mgame/rank/model"
)

type fakeRankRepo struct {
	update model.UpdateResult
	list   model.ListResult
	gotOff int
	gotLim int
}

func (f *fakeRankRepo) Update(context.Context, model.Board, model.Profile, model.UpdateMode, int64) (model.UpdateResult, error) {
	return f.update, nil
}
func (f *fakeRankRepo) List(_ context.Context, _ model.Board, offset, limit int, _ int64) (model.ListResult, error) {
	f.gotOff, f.gotLim = offset, limit
	return f.list, nil
}

type fakeRankNotifier struct {
	calls int
}

func (f *fakeRankNotifier) RankChanged(int64, model.Board, int32, int32, int64) { f.calls++ }

func TestRankManagerNotifiesOnRankChange(t *testing.T) {
	r := &fakeRankRepo{update: model.UpdateResult{OldRank: 3, NewRank: 1, Score: 10}}
	m := NewRankManager(r, 20, 100)
	n := &fakeRankNotifier{}
	m.SetNotifier(n)

	if _, err := m.Update(context.Background(), model.BoardPower, model.Profile{RoleID: 7}, model.UpdateOverwrite, 10); err != nil {
		t.Fatal(err)
	}
	if n.calls != 1 {
		t.Fatalf("notifier calls=%d", n.calls)
	}
}

func TestRankManagerSkipsNotifyWhenRankUnchanged(t *testing.T) {
	r := &fakeRankRepo{update: model.UpdateResult{OldRank: 1, NewRank: 1, Score: 10}}
	m := NewRankManager(r, 20, 100)
	n := &fakeRankNotifier{}
	m.SetNotifier(n)

	if _, err := m.Update(context.Background(), model.BoardPower, model.Profile{RoleID: 7}, model.UpdateOverwrite, 10); err != nil {
		t.Fatal(err)
	}
	if n.calls != 0 {
		t.Fatalf("notifier calls=%d", n.calls)
	}
}

func TestRankManagerClampsListLimit(t *testing.T) {
	r := &fakeRankRepo{}
	m := NewRankManager(r, 20, 100)
	if _, err := m.List(context.Background(), model.BoardPower, -1, 1000, 0); err != nil {
		t.Fatal(err)
	}
	if r.gotOff != 0 || r.gotLim != 100 {
		t.Fatalf("offset=%d limit=%d", r.gotOff, r.gotLim)
	}
}
