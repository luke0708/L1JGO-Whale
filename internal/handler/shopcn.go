package handler

import (
	"fmt"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// CnCurrencyType 天寶幣貨幣代碼（客戶端用此識別天寶幣商城）
const CnCurrencyType = 0x17d4

// CnCurrencyItemID 天寶幣物品 ID
const CnCurrencyItemID int32 = 6100

// handleCnShopBuy 顯示寄賣商城物品清單（玩家購買頁面）。
// 格式與 handleShopBuy 相同（opcode 70），但貨幣尾碼為 0x17d4。
// Java 參考: S_ShopSellListCn.java
func handleCnShopBuy(sess *net.Session, npcID, objID int32, deps *Deps) {
	if deps.ShopCn == nil {
		sendNoSell(sess, objID)
		return
	}

	items := deps.ShopCn.Get(npcID)
	if len(items) == 0 {
		sendNoSell(sess, objID)
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SELL_LIST) // opcode 70
	w.WriteD(objID)
	w.WriteH(uint16(len(items)))

	for i, cnItem := range items {
		itemInfo := deps.Items.Get(cnItem.ItemID)
		name := fmt.Sprintf("item#%d", cnItem.ItemID)
		gfxID := int32(0)
		if itemInfo != nil {
			name = itemInfo.Name
			gfxID = itemInfo.InvGfx
		}

		// 名稱前綴強化等級
		if cnItem.EnchantLevel > 0 {
			name = fmt.Sprintf("+%d %s", cnItem.EnchantLevel, name)
		}

		// 套裝數量標示
		if cnItem.PackCount > 1 {
			name = fmt.Sprintf("%s (%d)", name, cnItem.PackCount)
		}

		w.WriteD(int32(i + 1)) // 1-based 序號
		w.WriteH(uint16(gfxID))
		w.WriteD(cnItem.SellingPrice)
		w.WriteS(name)

		// 物品狀態位元組
		if itemInfo != nil {
			status := buildShopStatusBytes(itemInfo)
			w.WriteC(byte(len(status)))
			w.WriteBytes(status)
		} else {
			w.WriteC(0)
		}
	}

	w.WriteH(CnCurrencyType) // 天寶幣

	// 儲存商品對應表供購買時查詢（Java: pc.get_otherList().add_cnList）
	player.CnShopNpcID = npcID

	sess.Send(w.Bytes())
}

// handleCnShopSell 顯示回收清單（玩家背包中可回收的商城物品）。
// 格式與 handleShopSell 相同（opcode 65），但貨幣尾碼為 0x17d4。
// Java 參考: S_ShopBuyListCn.java
func handleCnShopSell(sess *net.Session, npcID, objID int32, deps *Deps) {
	if deps.ShopCn == nil {
		sendNoSell(sess, objID)
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Inv == nil {
		sendNoSell(sess, objID)
		return
	}

	// 掃描玩家背包，找出可回收的商城物品
	type assessedItem struct {
		objectID int32
		price    int32
	}
	var items []assessedItem

	for _, invItem := range player.Inv.Items {
		// 排除：已裝備、封印、不可交易
		if invItem.Equipped {
			continue
		}
		if invItem.Bless >= 128 {
			continue
		}

		// 排除：金幣、天寶幣
		if invItem.ItemID == world.AdenaItemID || invItem.ItemID == CnCurrencyItemID {
			continue
		}

		// 排除：不可交易的物品
		info := deps.Items.Get(invItem.ItemID)
		if info != nil && !info.Tradeable {
			continue
		}

		// 查詢回收價
		recyclePrice := deps.ShopCn.GetRecyclePrice(invItem.ItemID)
		if recyclePrice <= 0 {
			continue
		}

		items = append(items, assessedItem{objectID: invItem.ObjectID, price: recyclePrice})
	}

	if len(items) == 0 {
		sendNoSell(sess, objID)
		return
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SHOP_SELL_LIST) // opcode 65
	w.WriteD(objID)
	w.WriteH(uint16(len(items)))
	for _, it := range items {
		w.WriteD(it.objectID)
		w.WriteD(it.price)
	}
	w.WriteH(CnCurrencyType) // 天寶幣

	sess.Send(w.Bytes())
}

// handleCnBuyResult 處理玩家購買寄賣商城物品。
// Java 參考: C_Result.mode_cn_buy
// 封包格式：per item: readD(orderId) + readD(count)
func handleCnBuyResult(sess *net.Session, r *packet.Reader, count int, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if deps.ShopCn == nil {
		return
	}

	items := deps.ShopCn.Get(player.CnShopNpcID)
	if len(items) == 0 {
		return
	}

	for i := 0; i < count; i++ {
		orderID := int(r.ReadD()) // 1-based 序號
		buyCount := r.ReadD()

		if buyCount <= 0 {
			continue
		}

		// 序號轉索引（1-based → 0-based）
		idx := orderID - 1
		if idx < 0 || idx >= len(items) {
			continue
		}

		cnItem := items[idx]

		// 計算實際數量（考慮 packCount）
		actualCount := buyCount
		if cnItem.PackCount > 1 {
			actualCount = buyCount * cnItem.PackCount
		}

		// 計算總價
		totalPrice := int64(cnItem.SellingPrice) * int64(buyCount)
		if totalPrice > 2_000_000_000 {
			SendServerMessageArgs(sess, 904, "2000000000")
			return
		}
		price := int32(totalPrice)

		// 驗證天寶幣餘額
		currency := player.Inv.FindByItemID(CnCurrencyItemID)
		if currency == nil || currency.Count < price {
			sendServerMessage(sess, 189) // 金幣不足（天寶幣不足）
			return
		}

		// 驗證背包容量
		if player.Inv.IsFull() {
			sendServerMessage(sess, 270) // 背包已滿
			return
		}

		// 委派給系統執行購買
		deps.ShopCnMgr.BuyCnItem(sess, player, cnItem, buyCount, actualCount)
	}
}

// handleCnSellResult 處理玩家回收商城物品（賣給 NPC 換天寶幣）。
// Java 參考: C_Result.mode_cn_sell
// 封包格式：per item: readD(itemObjID) + readD(count)
func handleCnSellResult(sess *net.Session, r *packet.Reader, count int, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if deps.ShopCn == nil {
		return
	}

	for i := 0; i < count; i++ {
		itemObjID := r.ReadD()
		sellCount := r.ReadD()

		if sellCount <= 0 {
			continue
		}

		item := player.Inv.FindByObjectID(itemObjID)
		if item == nil {
			continue
		}

		// 驗證物品可回收
		if item.Equipped || item.Bless >= 128 {
			continue
		}

		recyclePrice := deps.ShopCn.GetRecyclePrice(item.ItemID)
		if recyclePrice <= 0 {
			continue
		}

		// 委派給系統執行回收
		deps.ShopCnMgr.SellCnItem(sess, player, item, sellCount, recyclePrice)
	}
}
