package persist

import "context"

// QuestRepo 提供 character_quests 資料表的最小 CRUD。
// 目前僅用於「欄位開通」等完成型任務（status=1=completed）。
type QuestRepo struct {
	db *DB
}

// NewQuestRepo 建立 QuestRepo。
func NewQuestRepo(db *DB) *QuestRepo {
	return &QuestRepo{db: db}
}

// LoadCompleted 載入角色所有已完成（status=1）的任務 ID。
// 回傳 map[questID]true，供 PlayerInfo.QuestsDone 使用。
func (r *QuestRepo) LoadCompleted(ctx context.Context, charID int32) (map[int32]bool, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT quest_id FROM character_quests WHERE char_id = $1 AND status = 1`, charID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int32]bool)
	for rows.Next() {
		var qid int32
		if err := rows.Scan(&qid); err != nil {
			return nil, err
		}
		result[qid] = true
	}
	return result, rows.Err()
}

// SetCompleted 將任務標記為已完成（INSERT 或 UPDATE）。
func (r *QuestRepo) SetCompleted(ctx context.Context, charID int32, questID int32) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO character_quests (char_id, quest_id, step, status, completed_at)
		 VALUES ($1, $2, 255, 1, NOW())
		 ON CONFLICT (char_id, quest_id) DO UPDATE
		 SET status = 1, step = 255, completed_at = NOW()`,
		charID, questID,
	)
	return err
}
