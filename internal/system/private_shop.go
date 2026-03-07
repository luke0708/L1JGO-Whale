package system

import (
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
)

// PrivateShopSystem 處理個人商店交易邏輯。
// 實作 handler.PrivateShopManager 介面。
type PrivateShopSystem struct {
	deps *handler.Deps
}

// NewPrivateShopSystem 建立個人商店交易系統。
func NewPrivateShopSystem(deps *handler.Deps) *PrivateShopSystem {
	return &PrivateShopSystem{deps: deps}
}

// TransferItem 從來源玩家背包移動物品到目標玩家背包。
func (s *PrivateShopSystem) TransferItem(from, to *world.PlayerInfo, item *world.InvItem, count int32) {
	info := s.deps.Items.Get(item.ItemID)

	if item.Stackable && item.Count > count {
		// 可堆疊物品：扣減來源數量 + 更新顯示
		item.Count -= count
		from.Dirty = true
		handler.SendItemCountUpdate(from.Session, item)

		// 目標：增加數量或新增
		destItem := to.Inv.AddItem(item.ItemID, count, item.Name, item.InvGfx, item.Weight, true, item.Bless)
		to.Dirty = true
		handler.SendAddItem(to.Session, destItem, info)
	} else {
		// 不可堆疊或全部移出：移除整個物品
		from.Inv.RemoveItem(item.ObjectID, count)
		from.Dirty = true
		handler.SendRemoveInventoryItem(from.Session, item.ObjectID)

		// 目標：新增物品（保留原有屬性）
		newItem := to.Inv.AddItemWithID(0, item.ItemID, count, item.Name, item.InvGfx, item.Weight, item.Stackable, item.Bless)
		newItem.EnchantLvl = item.EnchantLvl
		newItem.Identified = item.Identified
		newItem.UseType = item.UseType
		newItem.AttrEnchantKind = item.AttrEnchantKind
		newItem.AttrEnchantLevel = item.AttrEnchantLevel
		newItem.Durability = item.Durability
		to.Dirty = true
		handler.SendAddItem(to.Session, newItem, info)
	}
}

// TransferGold 轉移金幣。
func (s *PrivateShopSystem) TransferGold(from, to *world.PlayerInfo, amount int32) {
	// 從來源扣除
	fromAdena := from.Inv.FindByItemID(world.AdenaItemID)
	if fromAdena == nil {
		return
	}
	fromAdena.Count -= amount
	from.Dirty = true
	if fromAdena.Count <= 0 {
		from.Inv.RemoveItem(fromAdena.ObjectID, 0)
		handler.SendRemoveInventoryItem(from.Session, fromAdena.ObjectID)
	} else {
		handler.SendItemCountUpdate(from.Session, fromAdena)
	}

	// 給目標增加
	info := s.deps.Items.Get(world.AdenaItemID)
	toAdena := to.Inv.AddItem(world.AdenaItemID, amount, "金幣", 0, 0, true, 0)
	if info != nil {
		toAdena.InvGfx = info.InvGfx
		toAdena.Weight = info.Weight
		toAdena.Name = info.Name
	}
	to.Dirty = true
	handler.SendAddItem(to.Session, toAdena, info)
}
