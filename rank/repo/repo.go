package repo

import (
	"context"
	"fmt"

	"github.com/zhenhuasu/mgame/rank/model"
)

type RankRepo interface {
	Update(ctx context.Context, board model.Board, profile model.Profile, mode model.UpdateMode, value int64) (model.UpdateResult, error)
	List(ctx context.Context, board model.Board, offset, limit int, roleID int64) (model.ListResult, error)
}

func errBoard(board model.Board) error {
	return fmt.Errorf("unsupported rank board %d", board)
}
