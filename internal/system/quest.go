package system

import (
	"context"
	"time"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// QuestSystem 處理任務動作的所有遊戲邏輯（驗證、消耗、獎勵、步驟推進）。
// 實作 handler.QuestActionHandler 介面。
type QuestSystem struct {
	deps *handler.Deps
}

// NewQuestSystem 建立任務系統。
func NewQuestSystem(deps *handler.Deps) *QuestSystem {
	return &QuestSystem{deps: deps}
}

// ExecuteQuestAction 執行任務 NPC 動作：驗證條件 → 消耗道具 → 給予獎勵 → 推進步驟。
// Java: C_NPCAction 中的任務分支邏輯。
func (s *QuestSystem) ExecuteQuestAction(sess *net.Session, player *world.PlayerInfo, objID int32, npcID int32, action string) bool {
	if s.deps.QuestData == nil {
		return false
	}

	dialog, act := s.deps.QuestData.GetNpcAction(npcID, action)
	if act == nil {
		return false
	}

	// 檢查任務範本是否存在且啟用
	quest := s.deps.QuestData.GetQuest(dialog.QuestID)
	if quest == nil || !quest.Enabled {
		return true // 任務已停用，靜默處理
	}

	currentStep := player.QuestStep(dialog.QuestID)

	// 檢查步驟條件
	if act.RequiresStep != 0 && currentStep != act.RequiresStep {
		if act.FailHtml != "" {
			handler.SendHypertext(sess, objID, act.FailHtml)
		}
		return true
	}

	// 檢查等級條件
	if act.MinLevel > 0 && int32(player.Level) < act.MinLevel {
		if act.FailHtml != "" {
			handler.SendHypertext(sess, objID, act.FailHtml)
		} else {
			handler.SendHypertext(sess, objID, "y_q_not1") // 等級不足
		}
		return true
	}

	// 檢查職業條件
	if act.ClassMask > 0 {
		bit := int32(1) << uint(int(player.ClassType))
		if act.ClassMask&bit == 0 {
			if act.FailHtml != "" {
				handler.SendHypertext(sess, objID, act.FailHtml)
			} else {
				handler.SendHypertext(sess, objID, "y_q_not2") // 職業不符
			}
			return true
		}
	}

	// 檢查需持有的物品
	if len(act.RequireItems) > 0 {
		for _, req := range act.RequireItems {
			item := player.Inv.FindByItemID(req.ItemID)
			if item == nil || item.Count < req.Count {
				if act.FailHtml != "" {
					handler.SendHypertext(sess, objID, act.FailHtml)
				}
				return true
			}
		}
	}

	// ── 條件全部通過，執行動作 ──

	// 扣除物品
	for _, consume := range act.ConsumeItems {
		s.removeQuestItem(sess, player, consume.ItemID, consume.Count)
	}

	// 給予物品
	for _, give := range act.GiveItems {
		s.giveQuestItem(sess, player, give.ItemID, give.Count)
	}

	// 給予經驗值
	if act.GiveExp > 0 {
		player.Exp += act.GiveExp
		player.Dirty = true
	}

	// 給予金幣
	if act.GiveGold > 0 {
		s.giveQuestGold(sess, player, act.GiveGold)
	}

	// 設定任務步驟
	if act.SetStep > 0 {
		player.SetQuestStep(dialog.QuestID, act.SetStep)
		player.Dirty = true

		// 持久化到 DB
		if s.deps.QuestRepo != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := s.deps.QuestRepo.SetStep(ctx, player.CharID, dialog.QuestID, act.SetStep)
			cancel()
			if err != nil {
				s.deps.Log.Error("任務步驟寫入失敗",
					zap.Int32("charID", player.CharID),
					zap.Int32("questID", dialog.QuestID),
					zap.Int32("step", act.SetStep),
					zap.Error(err),
				)
			}
		}
	}

	// 傳送
	if act.TeleportTo != nil {
		handler.TeleportPlayer(sess, player, act.TeleportTo.X, act.TeleportTo.Y, act.TeleportTo.MapID, 5, s.deps)
	}

	// 顯示成功對話
	if act.SuccessHtml != "" {
		handler.SendHypertext(sess, objID, act.SuccessHtml)
	}

	s.deps.Log.Info("任務動作完成",
		zap.String("player", player.Name),
		zap.Int32("questID", dialog.QuestID),
		zap.String("action", action),
		zap.Int32("newStep", act.SetStep),
	)

	return true
}

// removeQuestItem 從背包扣除指定物品。
func (s *QuestSystem) removeQuestItem(sess *net.Session, player *world.PlayerInfo, itemID, count int32) {
	item := player.Inv.FindByItemID(itemID)
	if item == nil {
		return
	}
	removed := player.Inv.RemoveItem(item.ObjectID, count)
	if removed {
		handler.SendRemoveInventoryItem(sess, item.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, item)
	}
	player.Dirty = true
}

// giveQuestItem 給予玩家物品（任務獎勵）。
func (s *QuestSystem) giveQuestItem(sess *net.Session, player *world.PlayerInfo, itemID, count int32) {
	itemInfo := s.deps.Items.Get(itemID)
	if itemInfo == nil {
		s.deps.Log.Warn("任務獎勵物品不存在", zap.Int32("itemID", itemID))
		return
	}

	// 可堆疊物品：合併到已有堆疊
	if itemInfo.Stackable {
		existing := player.Inv.FindByItemID(itemID)
		if existing != nil {
			existing.Count += count
			handler.SendItemCountUpdate(sess, existing)
			player.Dirty = true
			return
		}
	}

	newItem := player.Inv.AddItem(
		itemID, count, itemInfo.Name, itemInfo.InvGfx,
		itemInfo.Weight, itemInfo.Stackable, 0,
	)
	handler.SendAddItem(sess, newItem, itemInfo)
	player.Dirty = true
}

// giveQuestGold 給予玩家金幣（任務獎勵）。
func (s *QuestSystem) giveQuestGold(sess *net.Session, player *world.PlayerInfo, amount int32) {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena != nil {
		adena.Count += amount
		handler.SendItemCountUpdate(sess, adena)
	} else {
		s.giveQuestItem(sess, player, world.AdenaItemID, amount)
	}
	player.Dirty = true
}
