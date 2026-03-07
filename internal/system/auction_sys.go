package system

import (
	"context"
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/persist"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// 拍賣結算間隔（每 5 分鐘 = 1500 ticks @ 200ms/tick）
const auctionSettleTicks = 1500

// AuctionSystem 血盟小屋拍賣系統。
// Phase 3 System：記憶體快取 + 出價 + 定時結算。
type AuctionSystem struct {
	deps    *handler.Deps
	ws      *world.State
	repo    *persist.AuctionRepo
	log     *zap.Logger
	elapsed int

	// 記憶體快取：houseID → entry
	entries map[int32]*persist.AuctionEntry
}

// NewAuctionSystem 建構拍賣系統（啟動時從 DB 載入所有拍賣記錄）。
func NewAuctionSystem(ws *world.State, deps *handler.Deps, repo *persist.AuctionRepo) *AuctionSystem {
	s := &AuctionSystem{
		deps:    deps,
		ws:      ws,
		repo:    repo,
		log:     deps.Log,
		entries: make(map[int32]*persist.AuctionEntry),
	}

	// 載入所有拍賣記錄到記憶體
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	list, err := repo.LoadAll(ctx)
	if err != nil {
		deps.Log.Error("載入拍賣資料失敗", zap.Error(err))
		return s
	}
	for i := range list {
		e := list[i]
		s.entries[e.HouseID] = &e
	}
	deps.Log.Info("拍賣資料載入完成", zap.Int("筆數", len(s.entries)))
	return s
}

func (s *AuctionSystem) Phase() coresys.Phase { return coresys.PhasePostUpdate }

func (s *AuctionSystem) Update(_ time.Duration) {
	s.elapsed++
	if s.elapsed < auctionSettleTicks {
		return
	}
	s.elapsed = 0
	s.settleExpired()
}

// --- AuctionManager 介面實作 ---

// GetEntriesForTown 取得指定城鎮的拍賣列表（依 NPC 座標過濾）。
func (s *AuctionSystem) GetEntriesForTown(npcX, npcY int32) []*persist.AuctionEntry {
	min, max := handler.TownHouseRange(npcX, npcY)
	if min == 0 && max == 0 {
		return nil
	}
	var result []*persist.AuctionEntry
	for _, e := range s.entries {
		if e.HouseID >= min && e.HouseID <= max {
			result = append(result, e)
		}
	}
	return result
}

// GetEntry 取得指定小屋的拍賣記錄。
func (s *AuctionSystem) GetEntry(houseID int32) *persist.AuctionEntry {
	return s.entries[houseID]
}

// PlaceBid 出價（含 WAL 保護）。
func (s *AuctionSystem) PlaceBid(sess *net.Session, player *world.PlayerInfo, houseID int32, amount int64) bool {
	entry := s.entries[houseID]
	if entry == nil {
		return false
	}

	// 驗證出價金額
	minBid := entry.Price
	if entry.BidderID != 0 {
		minBid = entry.Price + 1
	}
	if amount < minBid {
		handler.SendServerMessage(sess, 524) // "出價金額不足"
		return false
	}
	if amount > 2000000000 {
		amount = 2000000000
	}

	// 驗證金幣（adena 為 int32，拍賣金額上限 2B 在 int32 範圍內）
	bidAmount := int32(amount)
	currentGold := player.Inv.GetAdena()
	if currentGold < bidAmount {
		handler.SendServerMessage(sess, 189) // "金幣不足"
		return false
	}

	// WAL 保護：先寫 DB 再改記憶體
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 退還前一位競標者的金幣
	if entry.BidderID != 0 {
		prevBidder := s.ws.GetByName(entry.Bidder)
		if prevBidder != nil {
			// 線上玩家：直接加記憶體
			addAdena(prevBidder, int32(entry.Price))
			handler.SendAdenaUpdate(prevBidder.Session, prevBidder)
			handler.SendServerMsgWithParam(prevBidder.Session, 525, player.Name) // "{0} 出了更高的價錢"
		} else {
			// 離線玩家：直接 DB UPDATE
			if err := s.repo.RefundOfflineGold(ctx, entry.BidderID, entry.Price); err != nil {
				s.log.Error("退還離線競標者金幣失敗",
					zap.Int32("charID", entry.BidderID),
					zap.Error(err))
			}
		}
	}

	// 更新 DB
	if err := s.repo.UpdateBid(ctx, houseID, amount, player.Name, player.CharID); err != nil {
		s.log.Error("更新拍賣出價失敗", zap.Error(err))
		return false
	}

	// 扣金幣（直接在 system 層操作，不再反向呼叫 handler）
	deductAdena(player, bidAmount)
	handler.SendAdenaUpdate(sess, player)

	// 更新記憶體快取
	entry.Price = amount
	entry.Bidder = player.Name
	entry.BidderID = player.CharID

	handler.SendServerMessage(sess, 526) // "出價成功"
	return true
}

// IsAlreadyBidding 檢查玩家是否已在其他拍賣中出價。
func (s *AuctionSystem) IsAlreadyBidding(charName string) bool {
	for _, e := range s.entries {
		if e.Bidder == charName {
			return true
		}
	}
	return false
}

// --- 結算邏輯 ---

// settleExpired 結算所有到期的拍賣。
// Java: AuctionTimeController — 4 種情況：
// 1. 有原屋主 + 有競標者 → 轉讓所有權，原屋主得 price × 0.9
// 2. 無原屋主 + 有競標者 → 直接獲得小屋
// 3. 有原屋主 + 無競標者 → 取消拍賣，小屋歸還原屋主
// 4. 無原屋主 + 無競標者 → 延期 1 天
func (s *AuctionSystem) settleExpired() {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, entry := range s.entries {
		if now.Before(entry.Deadline) {
			continue
		}

		hasOwner := entry.OldOwnerID != 0
		hasBidder := entry.BidderID != 0

		switch {
		case hasOwner && hasBidder:
			s.settleWithOwnerAndBidder(ctx, entry)
		case !hasOwner && hasBidder:
			s.settleNewOwner(ctx, entry)
		case hasOwner && !hasBidder:
			s.settleCancelAuction(ctx, entry)
		default:
			// 無屋主 + 無競標者 → 延期 1 天
			s.extendDeadline(ctx, entry)
		}
	}
}

// settleWithOwnerAndBidder 有原屋主+有競標者：轉讓所有權。
func (s *AuctionSystem) settleWithOwnerAndBidder(ctx context.Context, entry *persist.AuctionEntry) {
	// 原屋主得 price × 0.9（10% 手續費）
	refund := int64(float64(entry.Price) * 0.9)

	// 退金幣給原屋主
	oldOwner := s.ws.GetByName(entry.OldOwner)
	if oldOwner != nil {
		addAdena(oldOwner, int32(refund))
		handler.SendAdenaUpdate(oldOwner.Session, oldOwner)
	} else {
		_ = s.repo.RefundOfflineGold(ctx, entry.OldOwnerID, refund)
	}

	// 更新競標者的血盟 HasHouse
	bidder := s.ws.GetByName(entry.Bidder)
	if bidder != nil && bidder.ClanID > 0 {
		clan := s.ws.Clans.GetClan(bidder.ClanID)
		if clan != nil {
			clan.HasHouse = entry.HouseID
		}
	}

	// 清除原屋主血盟的 HasHouse
	if oldOwner != nil && oldOwner.ClanID > 0 {
		oldClan := s.ws.Clans.GetClan(oldOwner.ClanID)
		if oldClan != nil {
			oldClan.HasHouse = 0
		}
	}

	// 刪除拍賣記錄
	if err := s.repo.DeleteAuction(ctx, entry.HouseID); err != nil {
		s.log.Error("刪除拍賣記錄失敗", zap.Int32("houseID", entry.HouseID), zap.Error(err))
		return
	}
	delete(s.entries, entry.HouseID)

	s.log.Info("拍賣結標：轉讓",
		zap.Int32("houseID", entry.HouseID),
		zap.String("原屋主", entry.OldOwner),
		zap.String("得標者", entry.Bidder),
		zap.Int64("價格", entry.Price))
}

// settleNewOwner 無原屋主+有競標者：直接獲得小屋。
func (s *AuctionSystem) settleNewOwner(ctx context.Context, entry *persist.AuctionEntry) {
	// 更新競標者的血盟 HasHouse
	bidder := s.ws.GetByName(entry.Bidder)
	if bidder != nil && bidder.ClanID > 0 {
		clan := s.ws.Clans.GetClan(bidder.ClanID)
		if clan != nil {
			clan.HasHouse = entry.HouseID
		}
	}

	// 刪除拍賣記錄
	if err := s.repo.DeleteAuction(ctx, entry.HouseID); err != nil {
		s.log.Error("刪除拍賣記錄失敗", zap.Int32("houseID", entry.HouseID), zap.Error(err))
		return
	}
	delete(s.entries, entry.HouseID)

	s.log.Info("拍賣結標：新屋主",
		zap.Int32("houseID", entry.HouseID),
		zap.String("得標者", entry.Bidder),
		zap.Int64("價格", entry.Price))
}

// settleCancelAuction 有原屋主+無競標者：取消拍賣，歸還小屋。
func (s *AuctionSystem) settleCancelAuction(ctx context.Context, entry *persist.AuctionEntry) {
	// 刪除拍賣記錄
	if err := s.repo.DeleteAuction(ctx, entry.HouseID); err != nil {
		s.log.Error("刪除拍賣記錄失敗", zap.Int32("houseID", entry.HouseID), zap.Error(err))
		return
	}
	delete(s.entries, entry.HouseID)

	s.log.Info("拍賣取消：無人出價",
		zap.Int32("houseID", entry.HouseID),
		zap.String("原屋主", entry.OldOwner))
}

// extendDeadline 無屋主+無競標者：延期 1 天。
func (s *AuctionSystem) extendDeadline(ctx context.Context, entry *persist.AuctionEntry) {
	newDeadline := auctionNextMidnight(time.Now())
	if err := s.repo.UpdateDeadline(ctx, entry.HouseID, newDeadline); err != nil {
		s.log.Error("延期拍賣失敗", zap.Int32("houseID", entry.HouseID), zap.Error(err))
		return
	}
	entry.Deadline = newDeadline
}

// auctionNextMidnight 回傳下一個午夜時間。
func auctionNextMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}

// addAdena 為玩家增加金幣。
func addAdena(player *world.PlayerInfo, amount int32) {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena != nil {
		adena.Count += amount
	}
	player.Dirty = true
}

// deductAdena 扣除玩家金幣。
func deductAdena(player *world.PlayerInfo, amount int32) {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena == nil || adena.Count < amount {
		return
	}
	adena.Count -= amount
	player.Dirty = true
}
