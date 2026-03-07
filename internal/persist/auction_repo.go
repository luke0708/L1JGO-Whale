package persist

import (
	"context"
	"time"
)

// AuctionEntry 拍賣佈告欄的一筆記錄（血盟小屋拍賣）。
type AuctionEntry struct {
	HouseID    int32
	HouseName  string
	HouseArea  int32
	Deadline   time.Time
	Price      int64
	Location   string
	OldOwner   string
	OldOwnerID int32
	Bidder     string
	BidderID   int32
}

// AuctionRepo 拍賣佈告欄持久化操作。
type AuctionRepo struct {
	db *DB
}

// NewAuctionRepo 建構拍賣 repo。
func NewAuctionRepo(db *DB) *AuctionRepo {
	return &AuctionRepo{db: db}
}

// LoadAll 載入所有拍賣記錄。
func (r *AuctionRepo) LoadAll(ctx context.Context) ([]AuctionEntry, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT house_id, house_name, house_area, deadline, price, location,
		        old_owner, old_owner_id, bidder, bidder_id
		 FROM auction_board ORDER BY house_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AuctionEntry
	for rows.Next() {
		var e AuctionEntry
		if err := rows.Scan(&e.HouseID, &e.HouseName, &e.HouseArea,
			&e.Deadline, &e.Price, &e.Location,
			&e.OldOwner, &e.OldOwnerID, &e.Bidder, &e.BidderID); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// UpdateBid 更新出價資料。
func (r *AuctionRepo) UpdateBid(ctx context.Context, houseID int32, price int64, bidder string, bidderID int32) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE auction_board SET price=$1, bidder=$2, bidder_id=$3 WHERE house_id=$4`,
		price, bidder, bidderID, houseID)
	return err
}

// UpdateDeadline 延期拍賣。
func (r *AuctionRepo) UpdateDeadline(ctx context.Context, houseID int32, deadline time.Time) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE auction_board SET deadline=$1 WHERE house_id=$2`,
		deadline, houseID)
	return err
}

// DeleteAuction 刪除拍賣記錄（結標後）。
func (r *AuctionRepo) DeleteAuction(ctx context.Context, houseID int32) error {
	_, err := r.db.Pool.Exec(ctx,
		`DELETE FROM auction_board WHERE house_id=$1`, houseID)
	return err
}

// InsertAuction 新增拍賣記錄（出售小屋用）。
func (r *AuctionRepo) InsertAuction(ctx context.Context, e *AuctionEntry) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO auction_board (house_id, house_name, house_area, deadline, price, location,
		                            old_owner, old_owner_id, bidder, bidder_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (house_id) DO UPDATE SET
		   deadline=$4, price=$5, old_owner=$7, old_owner_id=$8, bidder=$9, bidder_id=$10`,
		e.HouseID, e.HouseName, e.HouseArea, e.Deadline, e.Price, e.Location,
		e.OldOwner, e.OldOwnerID, e.Bidder, e.BidderID)
	return err
}

// RefundOfflineGold 離線玩家退回金幣（直接 DB UPDATE）。
func (r *AuctionRepo) RefundOfflineGold(ctx context.Context, charID int32, amount int64) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE characters SET adena = adena + $1 WHERE id = $2`,
		amount, charID)
	return err
}
