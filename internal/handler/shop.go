package handler

import (
	"fmt"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// HandleBuySell processes C_BUY_SELL (opcode 161) — player confirms a shop transaction.
// Java name: C_Result. Handles buy (0), sell (1), and warehouse operations (2-9).
func HandleBuySell(sess *net.Session, r *packet.Reader, deps *Deps) {
	npcObjID := r.ReadD()
	resultType := r.ReadC()
	count := int(r.ReadH())

	// 強化物品商店攔截（resultType 12 = S_PowerItemList type）
	if resultType == 12 {
		player := deps.World.GetBySession(sess.ID)
		if player != nil {
			pNpc := deps.World.GetNpc(npcObjID)
			if pNpc != nil {
				handlePowerItemBuy(sess, r, count, player, pNpc, deps)
			}
		}
		return
	}

	// Warehouse operations (resultType 2-9) route through warehouse handler.
	// Must check BEFORE NPC/shop lookup — warehouse NPCs have no shop data.
	if resultType >= 2 {
		HandleWarehouseResult(sess, r, resultType, count, deps)
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	// 火神精煉攔截：玩家從火神精煉介面「賣出」物品 → 換取結晶
	// Java: S_ShopBuyListFireSmith 使用 S_OPCODE_SHOP_SELL_LIST，
	// 客戶端回傳 C_Result type=1（賣出），需攔截處理。
	if resultType == 1 && player.FireSmithNpcObjID != 0 && player.FireSmithNpcObjID == npcObjID {
		handleFireSmithSell(sess, r, count, player, npcObjID, deps)
		return
	}

	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		// 目標不是 NPC → 檢查是否為個人商店玩家
		shopPlayer := deps.World.GetByCharID(npcObjID)
		if shopPlayer != nil && shopPlayer.PrivateShop {
			switch resultType {
			case 0:
				HandlePrivateShopBuy(sess, r, count, player, shopPlayer, deps)
			case 1:
				HandlePrivateShopSell(sess, r, count, player, shopPlayer, deps)
			}
		}
		return
	}

	// 寄賣商城 NPC 路由
	if npc.Impl == "L1Cn" {
		switch resultType {
		case 0:
			handleCnBuyResult(sess, r, count, player, npc, deps)
		case 1:
			handleCnSellResult(sess, r, count, player, npc, deps)
		}
		return
	}

	shop := deps.Shops.Get(npc.NpcID)
	if shop == nil {
		return
	}

	if deps.Shop == nil {
		return
	}

	switch resultType {
	case 0:
		// Buy from NPC — player purchases items
		deps.Shop.BuyFromNpc(sess, r, count, player, shop)
	case 1:
		// Sell to NPC — player sells items
		deps.Shop.SellToNpc(sess, r, count, player, shop)
	}
}

// --- Inventory packet helpers ---

// sendAddItem sends S_ADD_ITEM (opcode 15) — new item appears in inventory.
// Optional itemInfo enables status bytes (item stats) for identified items.
func sendAddItem(sess *net.Session, item *world.InvItem, optInfo ...*data.ItemInfo) {
	var itemInfo *data.ItemInfo
	if len(optInfo) > 0 {
		itemInfo = optInfo[0]
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ADD_ITEM)
	w.WriteD(item.ObjectID)                     // item object ID
	w.WriteH(world.ItemDescID(item.ItemID))     // descId — Java: switch(itemId) for material items
	w.WriteC(item.UseType)                 // use type
	w.WriteC(0)                            // charge count
	w.WriteH(uint16(item.InvGfx))         // inventory graphic ID
	w.WriteC(world.EffectiveBless(item))   // bless: 3=unidentified, else actual
	w.WriteD(item.Count)                   // stack count
	w.WriteC(itemStatusX(item, itemInfo))  // itemStatusX
	w.WriteS(buildViewName(item, itemInfo)) // display name
	// Status bytes: include item stats for identified items
	if item.Identified && itemInfo != nil {
		statusBytes := buildStatusBytes(item, itemInfo)
		if len(statusBytes) > 0 {
			w.WriteC(byte(len(statusBytes)))
			w.WriteBytes(statusBytes)
		} else {
			w.WriteC(0)
		}
	} else {
		w.WriteC(0)
	}
	// 尾部固定 11 bytes（Java: S_AddItem 與 S_InvList 共用格式）
	w.WriteC(10) // 固定值 0x0A
	w.WriteH(0)
	w.WriteD(0)
	w.WriteD(0)
	sess.Send(w.Bytes())
}

// sendRemoveInventoryItem sends S_REMOVE_INVENTORY (opcode 57) — item removed.
func sendRemoveInventoryItem(sess *net.Session, objectID int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_REMOVE_INVENTORY)
	w.WriteD(objectID)
	sess.Send(w.Bytes())
}

// sendItemCountUpdate sends S_CHANGE_ITEM_USE (opcode 24) — update stack count.
func sendItemCountUpdate(sess *net.Session, item *world.InvItem) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHANGE_ITEM_USE)
	w.WriteD(item.ObjectID)
	w.WriteS(buildViewName(item, nil))
	w.WriteD(item.Count)
	w.WriteC(0) // status bytes length = 0
	sess.Send(w.Bytes())
}

// sendServerMessage sends S_MESSAGE_CODE (opcode 71) — system message by ID.
func sendServerMessage(sess *net.Session, msgID uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_MESSAGE_CODE)
	w.WriteH(msgID) // message ID in client string table
	w.WriteC(0)     // no arguments
	sess.Send(w.Bytes())
}

// sendServerMessageArgs sends S_MESSAGE_CODE (opcode 71) with string arguments.
// The client substitutes %0, %1, ... with the provided args.
func sendServerMessageArgs(sess *net.Session, msgID uint16, args ...string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_MESSAGE_CODE)
	w.WriteH(msgID)
	w.WriteC(byte(len(args)))
	for _, arg := range args {
		w.WriteS(arg)
	}
	sess.Send(w.Bytes())
}

// sendInvList sends S_ADD_INVENTORY_BATCH (opcode 5) — full inventory.
func sendInvList(sess *net.Session, inv *world.Inventory, items *data.ItemTable) {
	if inv == nil || len(inv.Items) == 0 {
		// Send empty list
		w := packet.NewWriterWithOpcode(packet.S_OPCODE_ADD_INVENTORY_BATCH)
		w.WriteC(0)
		sess.Send(w.Bytes())
		return
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ADD_INVENTORY_BATCH)
	w.WriteC(byte(len(inv.Items)))

	for _, item := range inv.Items {
		var itemInfo *data.ItemInfo
		if items != nil {
			itemInfo = items.Get(item.ItemID)
		}

		w.WriteD(item.ObjectID)
		w.WriteH(world.ItemDescID(item.ItemID))  // descId — Java: switch(itemId) for material items
		w.WriteC(item.UseType)                    // use type
		w.WriteC(0)                               // charge count
		w.WriteH(uint16(item.InvGfx))            // inv gfx
		w.WriteC(world.EffectiveBless(item))      // bless: 3=unidentified
		w.WriteD(item.Count)                      // count
		w.WriteC(itemStatusX(item, itemInfo))     // itemStatusX
		// 顯示名稱（含強化前綴、數量後綴、裝備後綴）
		viewName := buildViewName(item, itemInfo)
		w.WriteS(viewName)
		// 狀態欄位：僅已鑑定物品
		if item.Identified && itemInfo != nil {
			statusBytes := buildStatusBytes(item, itemInfo)
			if len(statusBytes) > 0 {
				w.WriteC(byte(len(statusBytes)))
				w.WriteBytes(statusBytes)
			} else {
				w.WriteC(0)
			}
		} else {
			w.WriteC(0)
		}
		// 尾部固定 11 bytes（Java: S_InvList / S_AddItem 共用）
		w.WriteC(10) // 固定值 0x0A
		w.WriteH(0)
		w.WriteD(0)
		w.WriteD(0)
	}

	sess.Send(w.Bytes())
}

// --- 匯出封裝（供 system 套件使用） ---

// SendAddItem 匯出 sendAddItem — 供 system 套件發送新物品到背包。
func SendAddItem(sess *net.Session, item *world.InvItem, optInfo ...*data.ItemInfo) {
	sendAddItem(sess, item, optInfo...)
}

// SendItemCountUpdate 匯出 sendItemCountUpdate — 供 system 套件更新物品數量。
func SendItemCountUpdate(sess *net.Session, item *world.InvItem) {
	sendItemCountUpdate(sess, item)
}

// SendRemoveInventoryItem 匯出 sendRemoveInventoryItem — 供 system 套件移除背包物品。
func SendRemoveInventoryItem(sess *net.Session, objectID int32) {
	sendRemoveInventoryItem(sess, objectID)
}

// SendServerMessage 匯出 sendServerMessage — 供 system 套件發送系統訊息。
func SendServerMessage(sess *net.Session, msgID uint16) {
	sendServerMessage(sess, msgID)
}

// SendServerMessageArgs 匯出 sendServerMessageArgs — 供 system 套件發送帶參數系統訊息。
func SendServerMessageArgs(sess *net.Session, msgID uint16, args ...string) {
	sendServerMessageArgs(sess, msgID, args...)
}

// handleFireSmithSell 處理火神精煉賣出 — 分解物品換取魔法結晶體。
// Java: S_ShopBuyListFireSmith 發送的「商店賣出列表」，客戶端回傳 C_Result type=1。
// 封包格式：[D objectID][D count] × N（與一般賣出相同）。
// 玩家「賣出」的物品會被移除，並獲得對應數量的魔法結晶體（item 41246）。
func handleFireSmithSell(sess *net.Session, r *packet.Reader, count int, player *world.PlayerInfo, npcObjID int32, deps *Deps) {
	// 清除火神精煉狀態
	player.FireSmithNpcObjID = 0

	if count <= 0 || count > 100 {
		return
	}
	if deps.FireCrystals == nil {
		return
	}

	const crystalItemID int32 = 41246 // 魔法結晶體

	type sellOrder struct {
		objectID int32
		qty      int32
	}
	orders := make([]sellOrder, 0, count)
	for i := 0; i < count; i++ {
		objID := r.ReadD()
		qty := r.ReadD()
		if qty <= 0 {
			qty = 1
		}
		orders = append(orders, sellOrder{objectID: objID, qty: qty})
	}

	var totalCrystals int32

	for _, o := range orders {
		invItem := player.Inv.FindByObjectID(o.objectID)
		if invItem == nil || invItem.Equipped {
			continue
		}

		itemInfo := deps.Items.Get(invItem.ItemID)
		if itemInfo == nil || itemInfo.Category == data.CategoryEtcItem {
			continue
		}

		// 計算基礎 item ID（去除祝福/詛咒偏移）
		lookupID := invItem.ItemID
		if invItem.Bless == 0 {
			candidateID := invItem.ItemID - 100000
			if ci := deps.Items.Get(candidateID); ci != nil && ci.Name == itemInfo.Name {
				lookupID = candidateID
			}
		} else if invItem.Bless == 2 {
			candidateID := invItem.ItemID - 200000
			if ci := deps.Items.Get(candidateID); ci != nil && ci.Name == itemInfo.Name {
				lookupID = candidateID
			}
		}

		entry := deps.FireCrystals.Get(lookupID)
		if entry == nil {
			continue
		}

		crystalCount := entry.GetCrystalCount(int(invItem.EnchantLvl), int(itemInfo.Category), itemInfo.SafeEnchant)
		if crystalCount <= 0 {
			continue
		}

		// 武器/防具不可堆疊，qty 固定為 1
		sellQty := o.qty
		if sellQty > invItem.Count {
			sellQty = invItem.Count
		}

		totalCrystals += crystalCount * sellQty

		// 移除物品
		deps.NpcSvc.ConsumeItem(sess, player, invItem.ObjectID, sellQty)
	}

	// 給予魔法結晶體
	if totalCrystals > 0 {
		// 使用空的 InvItem 佔位（Refine 會處理移除+給予，但此處已移除完畢）
		// 直接給予結晶體
		deps.NpcSvc.Refine(sess, player, nil, crystalItemID, totalCrystals)
	}
	deps.Log.Info(fmt.Sprintf("火神精煉  角色=%s  獲得結晶=%d", player.Name, totalCrystals))
}
