package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	goredis "github.com/redis/go-redis/v9"

	coreredis "github.com/zhenhuasu/mgame/core/redis"
	"github.com/zhenhuasu/mgame/rank/model"
)

const (
	keyPrefix  = "mgame:rank:"
	profileKey = "mgame:rank:profile"
)

var updateScript = goredis.NewScript(`
local old = redis.call('ZREVRANK', KEYS[1], ARGV[3])
if old == false then old = -1 end
local score
if ARGV[1] == '1' then
  score = tonumber(ARGV[2])
  redis.call('ZADD', KEYS[1], score, ARGV[3])
else
  score = redis.call('ZINCRBY', KEYS[1], ARGV[2], ARGV[3])
  if tonumber(score) <= 0 then
    redis.call('ZREM', KEYS[1], ARGV[3])
    score = 0
  end
end
redis.call('HSET', KEYS[2], ARGV[3], ARGV[4])
local new = redis.call('ZREVRANK', KEYS[1], ARGV[3])
if new == false then new = -1 end
return {old, new, score}
`)

type redisRepo struct {
	kv *coreredis.Client
}

func NewRedis(kv *coreredis.Client) RankRepo {
	return &redisRepo{kv: kv}
}

func (r *redisRepo) Update(ctx context.Context, board model.Board, profile model.Profile, mode model.UpdateMode, value int64) (model.UpdateResult, error) {
	name, ok := board.Key()
	if !ok {
		return model.UpdateResult{}, errBoard(board)
	}
	if profile.RoleID <= 0 {
		return model.UpdateResult{}, fmt.Errorf("invalid role_id %d", profile.RoleID)
	}
	if mode == model.UpdateOverwrite && value < 0 {
		return model.UpdateResult{}, fmt.Errorf("negative overwrite score")
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return model.UpdateResult{}, fmt.Errorf("marshal profile: %w", err)
	}
	modeArg := "0"
	if mode == model.UpdateOverwrite {
		modeArg = "1"
	}
	raw, err := updateScript.Run(ctx, r.kv.Raw(),
		[]string{keyPrefix + name, profileKey},
		modeArg, value, strconv.FormatInt(profile.RoleID, 10), string(data)).Slice()
	if err != nil {
		return model.UpdateResult{}, fmt.Errorf("update rank %s role=%d: %w", name, profile.RoleID, err)
	}
	if len(raw) != 3 {
		return model.UpdateResult{}, fmt.Errorf("update rank %s role=%d: unexpected result len=%d", name, profile.RoleID, len(raw))
	}
	old := int32(toInt64(raw[0]))
	newRank := int32(toInt64(raw[1]))
	score := int64(toFloat64(raw[2]))
	if old >= 0 {
		old++
	} else {
		old = 0
	}
	if newRank >= 0 {
		newRank++
	} else {
		newRank = 0
	}
	return model.UpdateResult{OldRank: old, NewRank: newRank, Score: score}, nil
}

func (r *redisRepo) List(ctx context.Context, board model.Board, offset, limit int, roleID int64) (model.ListResult, error) {
	name, ok := board.Key()
	if !ok {
		return model.ListResult{}, errBoard(board)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		return model.ListResult{}, fmt.Errorf("invalid limit %d", limit)
	}
	key := keyPrefix + name
	zs, err := r.kv.Raw().ZRevRangeWithScores(ctx, key, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return model.ListResult{}, fmt.Errorf("list rank %s: %w", name, err)
	}

	entries := make([]model.Entry, 0, len(zs))
	ids := make([]string, 0, len(zs))
	for _, z := range zs {
		id := fmt.Sprint(z.Member)
		ids = append(ids, id)
		entries = append(entries, model.Entry{
			RoleID: parseInt(id),
			Score:  int64(z.Score),
		})
	}
	if len(ids) > 0 {
		vals, err := r.kv.Raw().HMGet(ctx, profileKey, ids...).Result()
		if err != nil {
			return model.ListResult{}, fmt.Errorf("load rank profiles: %w", err)
		}
		for i, v := range vals {
			if v == nil {
				continue
			}
			var p model.Profile
			switch raw := v.(type) {
			case string:
				if err := json.Unmarshal([]byte(raw), &p); err != nil {
					continue
				}
			case []byte:
				if err := json.Unmarshal(raw, &p); err != nil {
					continue
				}
			default:
				continue
			}
			entries[i].Name = p.Name
			entries[i].Avatar = p.Avatar
			entries[i].Level = p.Level
		}
	}
	for i := range entries {
		entries[i].Rank = int32(offset + i + 1)
	}

	total, err := r.kv.Raw().ZCard(ctx, key).Result()
	if err != nil {
		return model.ListResult{}, fmt.Errorf("rank total %s: %w", name, err)
	}
	res := model.ListResult{Entries: entries, Total: int32(total)}
	if roleID > 0 {
		rank, err := r.kv.Raw().ZRevRank(ctx, key, strconv.FormatInt(roleID, 10)).Result()
		if err == nil {
			res.SelfRank = int32(rank + 1)
		} else if err != goredis.Nil {
			return model.ListResult{}, fmt.Errorf("self rank %s role=%d: %w", name, roleID, err)
		}
		score, err := r.kv.Raw().ZScore(ctx, key, strconv.FormatInt(roleID, 10)).Result()
		if err == nil {
			res.SelfScore = int64(score)
		} else if err != goredis.Nil {
			return model.ListResult{}, fmt.Errorf("self score %s role=%d: %w", name, roleID, err)
		}
	}
	return res, nil
}

func parseInt(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case []byte:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case []byte:
		f, _ := strconv.ParseFloat(string(n), 64)
		return f
	default:
		return 0
	}
}
