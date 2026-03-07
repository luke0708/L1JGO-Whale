package system

import (
	"fmt"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// SpellShopSystem 處理魔法商店購買邏輯。
// 實作 handler.SpellShopManager 介面。
type SpellShopSystem struct {
	deps *handler.Deps
}

// NewSpellShopSystem 建立魔法商店系統。
func NewSpellShopSystem(deps *handler.Deps) *SpellShopSystem {
	return &SpellShopSystem{deps: deps}
}

// BuySpells 購買並學習魔法（扣金幣+學習技能+特效）。
func (s *SpellShopSystem) BuySpells(sess *net.Session, player *world.PlayerInfo, validSpells []*data.SkillInfo, totalCost int32) {
	// 扣除金幣
	adenaItem := player.Inv.FindByItemID(world.AdenaItemID)
	if adenaItem == nil {
		return
	}
	adenaItem.Count -= totalCost
	if adenaItem.Count <= 0 {
		player.Inv.RemoveItem(adenaItem.ObjectID, 0)
		handler.SendRemoveInventoryItem(sess, adenaItem.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, adenaItem)
	}

	// 學習每個技能
	for _, skill := range validSpells {
		player.KnownSpells = append(player.KnownSpells, skill.SkillID)
		handler.SendAddSingleSkill(sess, skill)
	}

	// 播放學習音效（GFX 224）
	handler.SendSkillEffect(sess, player.CharID, 224)

	player.Dirty = true

	s.deps.Log.Info(fmt.Sprintf("玩家學習魔法  角色=%s  數量=%d  花費=%d", player.Name, len(validSpells), totalCost))
}
