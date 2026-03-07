package system

import (
	"math/rand"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// 釣魚常數
const (
	fishingMap      int16 = 5300
	fishingPoleA    int32 = 83014 // 裝上捲線器 A
	fishingPoleB    int32 = 83024 // 裝上捲線器 B
	fishingBaitID   int32 = 83002 // 營養釣餌
	fishingReach    int32 = 6
	fishingReachAdv int32 = 9
	fishingInterval int   = 400 // 80 秒 ÷ 200ms/tick
)

// fishingReward 釣魚獎勵物品。
type fishingReward struct {
	ItemID int32
	Weight int32 // 累計概率（百萬分比）
}

var fishingRewards = []fishingReward{
	{41298, 300000},  // 鱈魚（30%）
	{41299, 550000},  // 虎斑帶魚（25%）
	{41300, 750000},  // 鮪魚（20%）
	{41301, 850000},  // 發光的鱈魚（10%）
	{41302, 920000},  // 發光的虎斑帶魚（7%）
	{41303, 970000},  // 發光的鮪魚（5%）
	{41304, 1000000}, // 發光的大魚（3%）
}

// FishingSystem 處理釣魚邏輯。實作 handler.FishingManager 介面。
type FishingSystem struct {
	deps *handler.Deps
}

// NewFishingSystem 建立釣魚系統。
func NewFishingSystem(deps *handler.Deps) *FishingSystem {
	return &FishingSystem{deps: deps}
}

// StartFishing 開始釣魚（設定狀態 + 廣播動作）。
func (s *FishingSystem) StartFishing(player *world.PlayerInfo, item *world.InvItem) {
	dx, dy := handler.HeadingOffset(player.Heading)
	reach := fishingReach
	if item.ItemID == fishingPoleA || item.ItemID == fishingPoleB {
		reach = fishingReachAdv
	}
	fishX := player.X + dx*reach
	fishY := player.Y + dy*reach

	player.Fishing = true
	player.FishX = fishX
	player.FishY = fishY
	player.FishingPoleID = item.ItemID
	player.FishingTick = 0

	// 廣播釣魚動作
	handler.SendFishingAction(player.Session, player, fishX, fishY)
	nearby := s.deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, player.SessionID)
	for _, other := range nearby {
		handler.SendFishingAction(other.Session, player, fishX, fishY)
	}

	s.deps.Log.Debug("開始釣魚",
		zap.String("player", player.Name),
		zap.Int32("fishX", fishX),
		zap.Int32("fishY", fishY),
	)
}

// StopFishing 結束釣魚狀態。
func (s *FishingSystem) StopFishing(player *world.PlayerInfo, sendMsg bool) {
	player.Fishing = false
	player.FishX = -1
	player.FishY = -1
	player.FishingPoleID = 0
	player.FishingTick = 0

	if sendMsg {
		handler.SendServerMessage(player.Session, 1163) // "釣魚已經結束了"
	}
}

// Tick 每 tick 更新釣魚計時器。
func (s *FishingSystem) Tick(player *world.PlayerInfo) {
	if !player.Fishing {
		return
	}

	player.FishingTick++
	if player.FishingTick < fishingInterval {
		return
	}
	player.FishingTick = 0

	if player.Inv == nil {
		s.StopFishing(player, true)
		return
	}

	// 檢查餌料
	bait := player.Inv.FindByItemID(fishingBaitID)
	if bait == nil {
		s.StopFishing(player, true)
		return
	}

	// 消耗餌料
	s.deps.NpcSvc.ConsumeItem(player.Session, player, bait.ObjectID, 1)

	// 隨機獲得魚
	fish := rollFishReward()
	if fish != 0 {
		s.deps.Log.Debug("釣到魚",
			zap.String("player", player.Name),
			zap.Int32("itemID", fish),
		)
		// TODO: 完整版需要 ItemCreateManager 介面
	}
}

// rollFishReward 隨機抽取釣魚獎勵。
func rollFishReward() int32 {
	roll := rand.Intn(1000000)
	for _, reward := range fishingRewards {
		if int32(roll) < reward.Weight {
			return reward.ItemID
		}
	}
	return 0
}
