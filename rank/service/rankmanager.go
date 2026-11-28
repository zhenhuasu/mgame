package service

import (
	"context"
	"fmt"

	"github.com/zhenhuasu/mgame/rank/model"
	"github.com/zhenhuasu/mgame/rank/repo"
)

type Notifier interface {
	RankChanged(roleID int64, board model.Board, oldRank, newRank int32, score int64)
}

type RankManager struct {
	repo         repo.RankRepo
	defaultLimit int
	maxLimit     int
	notifier     Notifier
}

func NewRankManager(r repo.RankRepo, defaultLimit, maxLimit int) *RankManager {
	return &RankManager{
		repo:         r,
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
	}
}

func (m *RankManager) SetNotifier(n Notifier) { m.notifier = n }

func (m *RankManager) Update(ctx context.Context, board model.Board, profile model.Profile, mode model.UpdateMode, value int64) (model.UpdateResult, error) {
	if profile.RoleID <= 0 {
		return model.UpdateResult{}, fmt.Errorf("invalid role_id %d", profile.RoleID)
	}
	if _, ok := board.Key(); !ok {
		return model.UpdateResult{}, fmt.Errorf("unsupported rank board %d", board)
	}
	if mode != model.UpdateDelta && mode != model.UpdateOverwrite {
		return model.UpdateResult{}, fmt.Errorf("unsupported update mode %d", mode)
	}
	if mode == model.UpdateOverwrite && value < 0 {
		return model.UpdateResult{}, fmt.Errorf("negative overwrite score")
	}
	res, err := m.repo.Update(ctx, board, profile, mode, value)
	if err != nil {
		return model.UpdateResult{}, err
	}
	if m.notifier != nil && res.NewRank > 0 && res.OldRank != res.NewRank {
		m.notifier.RankChanged(profile.RoleID, board, res.OldRank, res.NewRank, res.Score)
	}
	return res, nil
}

func (m *RankManager) List(ctx context.Context, board model.Board, offset, limit int, roleID int64) (model.ListResult, error) {
	if _, ok := board.Key(); !ok {
		return model.ListResult{}, fmt.Errorf("unsupported rank board %d", board)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = m.defaultLimit
	}
	if limit > m.maxLimit {
		limit = m.maxLimit
	}
	return m.repo.List(ctx, board, offset, limit, roleID)
}
