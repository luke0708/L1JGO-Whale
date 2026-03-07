package persist

import (
	"context"
	"time"
)

// InnRoom 旅館房間記錄（Java: L1Inn）。
type InnRoom struct {
	NpcID      int32
	RoomNumber int32
	KeyID      int32     // 對應的鑰匙物品 ObjectID（0=無）
	LodgerID   int32     // 租用者角色 ID（0=空房）
	Hall       bool      // 是否為會議室
	DueTime    time.Time // 租約到期時間
}

// InnRepo 旅館房間持久化操作。
type InnRepo struct {
	db *DB
}

func NewInnRepo(db *DB) *InnRepo {
	return &InnRepo{db: db}
}

// LoadAll 載入所有旅館房間記錄。
func (r *InnRepo) LoadAll(ctx context.Context) ([]*InnRoom, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT npc_id, room_number, key_id, lodger_id, hall, due_time FROM inn_rooms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*InnRoom
	for rows.Next() {
		room := &InnRoom{}
		if err := rows.Scan(
			&room.NpcID, &room.RoomNumber, &room.KeyID,
			&room.LodgerID, &room.Hall, &room.DueTime,
		); err != nil {
			return nil, err
		}
		result = append(result, room)
	}
	return result, rows.Err()
}

// UpdateRoom 更新單一房間記錄。
func (r *InnRepo) UpdateRoom(ctx context.Context, room *InnRoom) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE inn_rooms SET key_id=$1, lodger_id=$2, hall=$3, due_time=$4
		 WHERE npc_id=$5 AND room_number=$6`,
		room.KeyID, room.LodgerID, room.Hall, room.DueTime,
		room.NpcID, room.RoomNumber)
	return err
}

// EnsureRooms 確保指定 NPC 有 16 個房間記錄（不存在時建立）。
func (r *InnRepo) EnsureRooms(ctx context.Context, npcID int32) error {
	for i := int32(0); i < 16; i++ {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO inn_rooms (npc_id, room_number) VALUES ($1, $2)
			 ON CONFLICT (npc_id, room_number) DO NOTHING`,
			npcID, i)
		if err != nil {
			return err
		}
	}
	return nil
}
