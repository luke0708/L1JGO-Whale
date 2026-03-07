package persist

import (
	"context"
	"time"
)

// HouseState 住宅動態狀態（從 DB 載入）。
type HouseState struct {
	HouseID            int32
	HouseName          string
	HouseArea          int32
	Location           string
	KeeperID           int32
	IsPurchaseBasement bool
	TaxDeadline        time.Time
}

// HouseRepo 住宅持久化操作。
type HouseRepo struct {
	db *DB
}

// NewHouseRepo 建構住宅 repo。
func NewHouseRepo(db *DB) *HouseRepo {
	return &HouseRepo{db: db}
}

// LoadAll 載入所有住宅動態狀態。
func (r *HouseRepo) LoadAll(ctx context.Context) ([]HouseState, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT house_id, house_name, house_area, location,
		        keeper_id, is_purchase_basement, tax_deadline
		 FROM houses ORDER BY house_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HouseState
	for rows.Next() {
		var h HouseState
		if err := rows.Scan(&h.HouseID, &h.HouseName, &h.HouseArea, &h.Location,
			&h.KeeperID, &h.IsPurchaseBasement, &h.TaxDeadline); err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// UpdateBasement 更新地下盟屋購買狀態。
func (r *HouseRepo) UpdateBasement(ctx context.Context, houseID int32, purchased bool) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE houses SET is_purchase_basement = $1 WHERE house_id = $2`,
		purchased, houseID)
	return err
}

// UpdateName 更新住宅名稱。
func (r *HouseRepo) UpdateName(ctx context.Context, houseID int32, name string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE houses SET house_name = $1 WHERE house_id = $2`,
		name, houseID)
	return err
}

// UpdateTaxDeadline 更新稅期。
func (r *HouseRepo) UpdateTaxDeadline(ctx context.Context, houseID int32, deadline time.Time) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE houses SET tax_deadline = $1 WHERE house_id = $2`,
		deadline, houseID)
	return err
}

// SyncKeeperID 同步管家 NPC ID（從 YAML 靜態資料寫入 DB）。
func (r *HouseRepo) SyncKeeperID(ctx context.Context, houseID int32, keeperID int32) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE houses SET keeper_id = $1 WHERE house_id = $2`,
		keeperID, houseID)
	return err
}
