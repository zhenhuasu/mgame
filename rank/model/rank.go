package model

type Board int32

const (
	BoardPower Board = 1
	BoardLevel Board = 2
)

func (b Board) Key() (string, bool) {
	switch b {
	case BoardPower:
		return "power", true
	case BoardLevel:
		return "level", true
	default:
		return "", false
	}
}

type Profile struct {
	RoleID int64  `json:"role_id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Level  int32  `json:"level"`
}

type Entry struct {
	Rank   int32
	RoleID int64
	Name   string
	Avatar string
	Level  int32
	Score  int64
}

type UpdateMode int32

const (
	UpdateDelta UpdateMode = iota
	UpdateOverwrite
)

type UpdateResult struct {
	OldRank int32
	NewRank int32
	Score   int64
}

type ListResult struct {
	Entries   []Entry
	Total     int32
	SelfRank  int32
	SelfScore int64
}
