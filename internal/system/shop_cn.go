package system

import (
	"fmt"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// ShopCnSystem 處理天寶幣商城交易邏輯。
// 實作 handler.ShopCnManager 介面。
type ShopCnSystem struct {
	deps *handler.Deps
}

// NewShopCnSystem 建立天寶幣商城系統。
func NewShopCnSystem(deps *handler.Deps) *ShopCnSystem {
	return &ShopCnSystem{deps: deps}
}

// BuyCnItem 購買天寶幣商城物品（扣幣+給物品）。
func (s *ShopCnSystem) BuyCnItem(sess *net.Session, player *world.PlayerInfo, cnItem *data.ShopCnItem, buyCount, actualCount int32) {
	totalPrice := int64(cnItem.SellingPrice) * int64(buyCount)
	if totalPrice > 2_000_000_000 {
		handler.SendServerMessageArgs(sess, 904, "2000000000")
		return
	}
	price := int32(totalPrice)

	// 驗證天寶幣餘額
	currency := player.Inv.FindByItemID(handler.CnCurrencyItemID)
	if currency == nil || currency.Count < price {
		handler.SendServerMessage(sess, 189)
		return
	}

	// 驗證背包容量
	if player.Inv.IsFull() {
		handler.SendServerMessage(sess, 270)
		return
	}

	// 扣除天寶幣
	currency.Count -= price
	player.Dirty = true
	if currency.Count <= 0 {
		player.Inv.RemoveItem(currency.ObjectID, 0)
		handler.SendRemoveInventoryItem(sess, currency.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, currency)
	}

	// 給予物品
	itemInfo := s.deps.Items.Get(cnItem.ItemID)
	itemName := fmt.Sprintf("item#%d", cnItem.ItemID)
	gfxID := int32(0)
	weight := int32(0)
	stackable := false
	bless := byte(0)
	if itemInfo != nil {
		itemName = itemInfo.Name
		gfxID = itemInfo.InvGfx
		weight = itemInfo.Weight
		stackable = itemInfo.Stackable
		bless = byte(itemInfo.Bless)
	}

	newItem := player.Inv.AddItemWithID(0, cnItem.ItemID, actualCount, itemName, gfxID, weight, stackable, bless)
	if cnItem.EnchantLevel > 0 {
		newItem.EnchantLvl = int8(cnItem.EnchantLevel)
	}
	handler.SendAddItem(sess, newItem, itemInfo)
}

// SellCnItem 回收物品換天寶幣（移除物品+給幣）。
func (s *ShopCnSystem) SellCnItem(sess *net.Session, player *world.PlayerInfo, item *world.InvItem, sellCount, recyclePrice int32) {
	// 限制回收數量
	if sellCount > item.Count {
		sellCount = item.Count
	}

	// 計算回收總價
	totalPrice := int64(recyclePrice) * int64(sellCount)
	if totalPrice > 2_000_000_000 {
		totalPrice = 2_000_000_000
	}
	price := int32(totalPrice)

	// 移除物品
	if item.Stackable && item.Count > sellCount {
		item.Count -= sellCount
		player.Dirty = true
		handler.SendItemCountUpdate(sess, item)
	} else {
		player.Inv.RemoveItem(item.ObjectID, sellCount)
		player.Dirty = true
		handler.SendRemoveInventoryItem(sess, item.ObjectID)
	}

	// 給予天寶幣
	currencyInfo := s.deps.Items.Get(handler.CnCurrencyItemID)
	currencyName := "天寶"
	gfxID := int32(0)
	weight := int32(0)
	if currencyInfo != nil {
		currencyName = currencyInfo.Name
		gfxID = currencyInfo.InvGfx
		weight = currencyInfo.Weight
	}

	coinItem := player.Inv.AddItem(handler.CnCurrencyItemID, price, currencyName, gfxID, weight, true, 0)
	handler.SendAddItem(sess, coinItem, currencyInfo)
}
