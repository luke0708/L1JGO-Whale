package persist

import "context"

// QuestRepo 提供 character_quests 資料表的完整 CRUD。
// Java: CharacterQuestTable + L1PcQuest
// step 約定：0=未開始, 1~254=進行中, 255=已完成
type QuestRepo struct {
	db *DB
}

// NewQuestRepo 建立 QuestRepo。
func NewQuestRepo(db *DB) *QuestRepo {
	return &QuestRepo{db: db}
}

// LoadAll 載入角色所有任務進度（quest_id → step）。
// 包含進行中和已完成的任務。
func (r *QuestRepo) LoadAll(ctx context.Context, charID int32) (map[int32]int32, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT quest_id, step FROM character_quests WHERE char_id = $1`, charID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int32]int32)
	for rows.Next() {
		var qid, step int32
		if err := rows.Scan(&qid, &step); err != nil {
			return nil, err
		}
		result[qid] = step
	}
	return result, rows.Err()
}

// SetStep 設定任務進度（INSERT 或 UPDATE）。
// step=255 時自動設定 status=1（已完成）和 completed_at。
func (r *QuestRepo) SetStep(ctx context.Context, charID, questID, step int32) error {
	status := int16(0)
	if step == 255 {
		status = 1
	}

	if step == 255 {
		_, err := r.db.Pool.Exec(ctx,
			`INSERT INTO character_quests (char_id, quest_id, step, status, completed_at)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (char_id, quest_id) DO UPDATE
			 SET step = $3, status = $4, completed_at = NOW()`,
			charID, questID, step, status,
		)
		return err
	}
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO character_quests (char_id, quest_id, step, status)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (char_id, quest_id) DO UPDATE
		 SET step = $3, status = $4`,
		charID, questID, step, status,
	)
	return err
}

// SetCompleted 將任務標記為已完成（step=255, status=1）。
// 保留給舊有呼叫方（attr.go 戒指欄位開通）。
func (r *QuestRepo) SetCompleted(ctx context.Context, charID int32, questID int32) error {
	return r.SetStep(ctx, charID, questID, 255)
}

// DeleteQuest 刪除任務記錄（用於任務重置或每日任務清除）。
func (r *QuestRepo) DeleteQuest(ctx context.Context, charID, questID int32) error {
	_, err := r.db.Pool.Exec(ctx,
		`DELETE FROM character_quests WHERE char_id = $1 AND quest_id = $2`,
		charID, questID,
	)
	return err
}
