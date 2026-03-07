package system

import (
	"fmt"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// PowerItemSystem 處理強化物品購買邏輯。
// 實作 handler.PowerItemManager 介面。
type PowerItemSystem struct {
	deps *handler.Deps
}

// NewPowerItemSystem 建立強化物品系統。
func NewPowerItemSystem(deps *handler.Deps) *PowerItemSystem {
	return &PowerItemSystem{deps: deps}
}

// BuyPowerItem 購買強化物品（扣金幣+給物品含屬性）。
func (s *PowerItemSystem) BuyPowerItem(sess *net.Session, player *world.PlayerInfo, pItem *data.PowerShopItem) {
	// 驗證金幣
	adena := player.Inv.GetAdena()
	if adena < pItem.Price {
		handler.SendServerMessage(sess, 189)
		return
	}

	// 驗證背包容量
	if player.Inv.IsFull() {
		handler.SendServerMessage(sess, 270)
		return
	}

	// 扣除金幣
	adenaItem := player.Inv.FindByItemID(world.AdenaItemID)
	if adenaItem == nil {
		return
	}
	adenaItem.Count -= pItem.Price
	player.Dirty = true
	if adenaItem.Count <= 0 {
		player.Inv.RemoveItem(adenaItem.ObjectID, 0)
		handler.SendRemoveInventoryItem(sess, adenaItem.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, adenaItem)
	}

	// 給予強化物品
	itemInfo := s.deps.Items.Get(pItem.ItemID)
	itemName := fmt.Sprintf("item#%d", pItem.ItemID)
	gfxID := int32(0)
	weight := int32(0)
	stackable := false
	bless := byte(pItem.Bless)
	if itemInfo != nil {
		itemName = itemInfo.Name
		gfxID = itemInfo.InvGfx
		weight = itemInfo.Weight
		stackable = itemInfo.Stackable
		if pItem.Bless == 0 {
			bless = byte(itemInfo.Bless)
		}
	}

	newItem := player.Inv.AddItemWithID(0, pItem.ItemID, 1, itemName, gfxID, weight, stackable, bless)
	newItem.EnchantLvl = int8(pItem.EnchantLvl)
	newItem.Identified = true
	if pItem.AttrKind > 0 {
		newItem.AttrEnchantKind = int8(pItem.AttrKind)
		newItem.AttrEnchantLevel = int8(pItem.AttrLevel)
	}
	handler.SendAddItem(sess, newItem, itemInfo)
}
