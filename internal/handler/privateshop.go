package handler

import (
	"fmt"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// HandleShop 處理 C_Shop (opcode 38) — 開設/取消個人商店。
// Java 參考: C_Shop.java
// 封包格式:
//
//	readC() → type (0=開設, 1=取消)
//	type 0:
//	  readH() → sellCount
//	  sellCount * (readD objectID, readD price, readD count)
//	  readH() → buyCount
//	  buyCount * (readD objectID, readD price, readD count)
//	  readByte() → shopChat (Big5 商店標語)
func HandleShop(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Dead {
		return
	}

	shopType := r.ReadC()

	switch shopType {
	case 0:
		openPrivateShop(sess, r, player, deps)
	case 1:
		closePrivateShop(player, deps)
	}
}

// openPrivateShop 開設個人商店。
func openPrivateShop(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	// 讀取出售清單
	sellCount := int(r.ReadH())
	if sellCount > 8 {
		sellCount = 8 // 最多 8 項（Java 硬上限）
	}

	sellList := make([]*world.PrivateShopSell, 0, sellCount)
	tradable := true

	for i := 0; i < sellCount; i++ {
		objID := r.ReadD()
		price := r.ReadD()
		count := r.ReadD()

		item := player.Inv.FindByObjectID(objID)
		if item == nil {
			continue
		}

		// 驗證：物品可交易性
		if !validateShopItem(sess, item, player, deps) {
			tradable = false
			continue
		}

		// 價格必須 > 0
		if price <= 0 {
			continue
		}

		// 數量不能超過持有量
		if count > item.Count {
			count = item.Count
		}
		if count <= 0 {
			continue
		}

		sellList = append(sellList, &world.PrivateShopSell{
			ItemObjectID: objID,
			SellTotal:    count,
			SellPrice:    price,
			SoldCount:    0,
		})
	}

	// 讀取收購清單
	buyCount := int(r.ReadH())
	if buyCount > 8 {
		buyCount = 8
	}

	buyList := make([]*world.PrivateShopBuy, 0, buyCount)
	for i := 0; i < buyCount; i++ {
		objID := r.ReadD()
		price := r.ReadD()
		count := r.ReadD()

		// 收購清單的 objID 是玩家背包中的「樣本」物品
		item := player.Inv.FindByObjectID(objID)
		if item == nil {
			continue
		}

		if price <= 0 || count <= 0 {
			continue
		}

		buyList = append(buyList, &world.PrivateShopBuy{
			ItemObjectID: objID,
			ItemID:       item.ItemID,
			EnchantLvl:   item.EnchantLvl,
			BuyTotal:     count,
			BuyPrice:     price,
			BoughtCount:  0,
		})
	}

	// 讀取商店標語（Big5 位元組）
	shopChat := r.ReadBytes(r.Remaining())

	if !tradable {
		// 有不可交易物品 → 取消擺攤
		player.PrivateShop = false
		player.ShopSellList = nil
		player.ShopBuyList = nil
		nearby := deps.World.GetNearbyPlayersAt(player.X, player.Y, player.MapID)
		BroadcastToPlayers(nearby, BuildActionGfx(player.CharID, 3)) // 取消動作
		return
	}

	if len(sellList) == 0 && len(buyList) == 0 {
		return
	}

	// 設定擺攤狀態
	player.PrivateShop = true
	player.ShopSellList = sellList
	player.ShopBuyList = buyList
	player.ShopChat = shopChat

	nearby := deps.World.GetNearbyPlayersAt(player.X, player.Y, player.MapID)

	// 廣播擺攤動作 + 標語（Java: S_DoActionShop — opcode 158, action 70, + shopChat）
	BroadcastToPlayers(nearby, BuildActionGfx(player.CharID, 3)) // 先取消原有動作
	shopData := buildShopAction(player.CharID, shopChat)
	BroadcastToPlayers(nearby, shopData)
}

// closePrivateShop 取消個人商店。
func closePrivateShop(player *world.PlayerInfo, deps *Deps) {
	player.PrivateShop = false
	player.ShopSellList = nil
	player.ShopBuyList = nil
	player.ShopChat = nil
	player.ShopTradingLocked = false

	nearby := deps.World.GetNearbyPlayersAt(player.X, player.Y, player.MapID)
	BroadcastToPlayers(nearby, BuildActionGfx(player.CharID, 3)) // 取消動作
}

// validateShopItem 驗證物品是否可在個人商店中出售。
func validateShopItem(sess *net.Session, item *world.InvItem, player *world.PlayerInfo, deps *Deps) bool {
	// 已裝備物品不可交易（Java: S_ServerMessage(141)）
	if item.Equipped {
		sendServerMessage(sess, 141)
		return false
	}

	// 被封印的物品不可交易（bless >= 128）
	if item.Bless >= 128 {
		sendServerMessage(sess, 1497)
		return false
	}

	// 查詢物品模板，檢查 Tradeable
	info := deps.Items.Get(item.ItemID)
	if info != nil && !info.Tradeable {
		sendServerMessage(sess, 1497)
		return false
	}

	return true
}

// HandleQueryPrivateShop 處理 C_ShopList (opcode 47) — 瀏覽他人的個人商店。
// Java 參考: C_ShopList.java
// 封包格式: readC(type) + readD(objectId)
// type: 0=出售清單, 1=收購清單
func HandleQueryPrivateShop(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Dead || player.PrivateShop {
		return // 自己開店中不可瀏覽他人商店
	}

	queryType := r.ReadC()
	objectID := r.ReadD()

	// 尋找目標玩家
	shopPlayer := deps.World.GetByCharID(objectID)
	if shopPlayer == nil || !shopPlayer.PrivateShop {
		return
	}

	switch queryType {
	case 0:
		sendPrivateShopSellList(sess, player, shopPlayer, deps)
	case 1:
		sendPrivateShopBuyList(sess, player, shopPlayer, deps)
	}
}

// sendPrivateShopSellList 發送出售商品清單。
// Java 參考: S_PrivateShop.isPc() type==0
func sendPrivateShopSellList(sess *net.Session, viewer, shopPlayer *world.PlayerInfo, deps *Deps) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PRIVATESHOPLIST)
	w.WriteC(0) // type = 出售
	w.WriteD(shopPlayer.CharID)

	list := shopPlayer.ShopSellList
	if len(list) == 0 {
		w.WriteH(0)
		sess.Send(w.Bytes())
		return
	}

	// 記錄對方商品數量（Java: setPartnersPrivateShopItemCount）
	viewer.ShopPartnerCount = len(list)

	w.WriteH(uint16(len(list)))
	for i, pssl := range list {
		item := shopPlayer.Inv.FindByObjectID(pssl.ItemObjectID)
		if item == nil {
			continue
		}

		remaining := pssl.SellTotal - pssl.SoldCount
		if remaining <= 0 {
			continue
		}

		w.WriteC(byte(i))               // order index
		w.WriteC(item.Bless)            // bless
		w.WriteH(uint16(item.InvGfx))   // gfx
		w.WriteD(remaining)             // 可購買數量
		w.WriteD(pssl.SellPrice)        // 單價
		w.WriteS(buildShopItemName(item, remaining)) // 顯示名稱
		w.WriteC(0)                     // 固定尾碼
	}

	sess.Send(w.Bytes())
}

// sendPrivateShopBuyList 發送收購商品清單。
// Java 參考: S_PrivateShop.isPc() type==1
// 收購清單：遍歷商店玩家的收購條目，在查詢者背包中找到匹配的物品。
func sendPrivateShopBuyList(sess *net.Session, viewer, shopPlayer *world.PlayerInfo, deps *Deps) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PRIVATESHOPLIST)
	w.WriteC(1) // type = 收購
	w.WriteD(shopPlayer.CharID)

	list := shopPlayer.ShopBuyList
	if len(list) == 0 {
		w.WriteH(0)
		sess.Send(w.Bytes())
		return
	}

	w.WriteH(uint16(len(list)))
	for i, psbl := range list {
		// 在商店玩家背包中找到樣本物品，取得 itemID 和 enchantLevel
		sampleItem := shopPlayer.Inv.FindByObjectID(psbl.ItemObjectID)
		if sampleItem == nil {
			continue
		}

		// 在查詢者背包中找到符合條件的物品
		for _, pcItem := range viewer.Inv.Items {
			if pcItem.ItemID == psbl.ItemID && pcItem.EnchantLvl == psbl.EnchantLvl {
				remaining := psbl.BuyTotal - psbl.BoughtCount
				if remaining <= 0 {
					break
				}
				w.WriteC(byte(i))           // order index
				w.WriteD(pcItem.ObjectID)   // 查詢者背包中的物品 ObjectID
				w.WriteD(remaining)         // 收購數量
				w.WriteD(psbl.BuyPrice)     // 收購單價
				break // 每個收購條目只匹配第一個符合的物品
			}
		}
	}

	sess.Send(w.Bytes())
}

// HandlePrivateShopBuy 處理從個人商店購買物品。
// 從 HandleBuySell 路由（當目標是玩家而非 NPC 時）。
// Java 參考: C_Result.mode_buypc
// 封包格式（已由 HandleBuySell 讀取 npcObjID/resultType/count）：
//
//	for count: readD(order) + readD(buyCount)
func HandlePrivateShopBuy(sess *net.Session, r *packet.Reader, itemCount int, player *world.PlayerInfo, shopPlayer *world.PlayerInfo, deps *Deps) {
	if shopPlayer.ShopTradingLocked {
		return // 另一玩家正在交易中
	}
	shopPlayer.ShopTradingLocked = true
	defer func() { shopPlayer.ShopTradingLocked = false }()

	sellList := shopPlayer.ShopSellList
	if len(sellList) == 0 {
		return
	}

	// 驗證商品數量一致性（Java: getPartnersPrivateShopItemCount）
	if player.ShopPartnerCount != len(sellList) {
		return
	}

	for n := 0; n < itemCount; n++ {
		order := int(r.ReadD())
		count := r.ReadD()

		if order < 0 || order >= len(sellList) || count <= 0 {
			continue
		}

		pssl := sellList[order]
		remaining := pssl.SellTotal - pssl.SoldCount
		if count > remaining {
			count = remaining
		}
		if count <= 0 {
			continue
		}

		item := shopPlayer.Inv.FindByObjectID(pssl.ItemObjectID)
		if item == nil {
			sendServerMessage(sess, 989) // 無法交易
			continue
		}

		// 價格溢位保護（Java: price * count > 2000000000）
		totalPrice := int64(pssl.SellPrice) * int64(count)
		if totalPrice > 2_000_000_000 {
			SendServerMessageArgs(sess, 904, "2000000000")
			return
		}
		price := int32(totalPrice)

		// 驗證買方金幣
		buyerAdena := player.Inv.GetAdena()
		if buyerAdena < price {
			sendServerMessage(sess, 189) // 金幣不足
			continue
		}

		// 驗證賣方物品數量
		if item.Count < count {
			sendServerMessage(sess, 989) // 無法交易
			continue
		}

		// 驗證買方背包容量
		if !item.Stackable && player.Inv.Size()+int(count) > world.MaxInventorySize {
			sendServerMessage(sess, 270) // 背包過重
			break
		}

		// 執行物品轉移：賣方 → 買方
		transferShopItem(shopPlayer, player, item, count, deps)

		// 執行金幣轉移：買方 → 賣方
		transferShopGold(player, shopPlayer, price, deps)

		// 通知商店玩家：出售成功（Java: S_ServerMessage 877）
		itemName := item.Name
		if count > 1 {
			itemName = fmt.Sprintf("%s (%d)", item.Name, count)
		}
		SendServerMessageArgs(shopPlayer.Session, 877, player.Name, itemName)

		// 更新售出累計
		pssl.SoldCount += count
	}

	// 清理已售完的項目（從末尾向前刪除）
	for i := len(sellList) - 1; i >= 0; i-- {
		if sellList[i].SoldCount >= sellList[i].SellTotal {
			sellList = append(sellList[:i], sellList[i+1:]...)
		}
	}
	shopPlayer.ShopSellList = sellList

	// 如果所有商品都售完，自動關閉商店
	if len(sellList) == 0 && len(shopPlayer.ShopBuyList) == 0 {
		closePrivateShop(shopPlayer, deps)
	}
}

// HandlePrivateShopSell 處理向個人商店出售物品（賣給商店的收購清單）。
// 從 HandleBuySell 路由（resultType == 1 且目標是玩家）。
// Java 參考: C_Result.mode_sellpc
// 封包格式：for count: readD(itemObjID) + readD(sellCount) + readC(order)
func HandlePrivateShopSell(sess *net.Session, r *packet.Reader, itemCount int, player *world.PlayerInfo, shopPlayer *world.PlayerInfo, deps *Deps) {
	if shopPlayer.ShopTradingLocked {
		return
	}
	shopPlayer.ShopTradingLocked = true
	defer func() { shopPlayer.ShopTradingLocked = false }()

	buyList := shopPlayer.ShopBuyList
	if len(buyList) == 0 {
		return
	}

	for n := 0; n < itemCount; n++ {
		itemObjID := r.ReadD()
		count := r.ReadD()
		order := int(r.ReadC())

		if order < 0 || order >= len(buyList) || count <= 0 {
			continue
		}

		psbl := buyList[order]
		remaining := psbl.BuyTotal - psbl.BoughtCount
		if count > remaining {
			count = remaining
		}
		if count <= 0 {
			continue
		}

		// 驗證玩家背包中的物品
		item := player.Inv.FindByObjectID(itemObjID)
		if item == nil {
			continue
		}

		// 驗證物品種類和強化等級匹配（防作弊）
		if item.ItemID != psbl.ItemID || item.EnchantLvl != psbl.EnchantLvl {
			return // 可能作弊
		}

		// 驗證物品數量
		if item.Count < count {
			sendServerMessage(sess, 989)
			continue
		}

		// 價格計算
		totalPrice := int64(psbl.BuyPrice) * int64(count)
		if totalPrice > 2_000_000_000 {
			SendServerMessageArgs(sess, 904, "2000000000")
			return
		}
		price := int32(totalPrice)

		// 驗證商店玩家金幣
		shopAdena := shopPlayer.Inv.GetAdena()
		if shopAdena < price {
			sendServerMessage(sess, 189) // 金幣不足
			break
		}

		// 驗證商店玩家背包容量
		if !item.Stackable && shopPlayer.Inv.Size()+int(count) > world.MaxInventorySize {
			sendServerMessage(sess, 271) // 對方背包過重
			break
		}

		// 執行物品轉移：賣方（玩家）→ 商店玩家
		transferShopItem(player, shopPlayer, item, count, deps)

		// 執行金幣轉移：商店玩家 → 玩家
		transferShopGold(shopPlayer, player, price, deps)

		// 更新收購累計
		psbl.BoughtCount += count
	}

	// 清理已收購完的項目
	for i := len(buyList) - 1; i >= 0; i-- {
		if buyList[i].BoughtCount >= buyList[i].BuyTotal {
			buyList = append(buyList[:i], buyList[i+1:]...)
		}
	}
	shopPlayer.ShopBuyList = buyList

	// 所有商品收購完成 → 自動關閉商店
	if len(shopPlayer.ShopSellList) == 0 && len(buyList) == 0 {
		closePrivateShop(shopPlayer, deps)
	}
}

// transferShopItem 從來源玩家背包移動物品到目標玩家背包。
func transferShopItem(from, to *world.PlayerInfo, item *world.InvItem, count int32, deps *Deps) {
	deps.PrivShop.TransferItem(from, to, item, count)
}

// transferShopGold 轉移金幣。
func transferShopGold(from, to *world.PlayerInfo, amount int32, deps *Deps) {
	deps.PrivShop.TransferGold(from, to, amount)
}

// buildShopAction 建構擺攤動作封包（Java: S_DoActionShop）。
// 格式：opcode 158 + writeD(objectID) + writeC(70) + writeByte(shopChat)
func buildShopAction(charID int32, shopChat []byte) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ACTION)
	w.WriteD(charID)
	w.WriteC(70) // 擺攤動作代碼
	if len(shopChat) > 0 {
		w.WriteBytes(shopChat)
	}
	return w.Bytes()
}

// buildShopItemName 建構商品顯示名稱（帶數量）。
func buildShopItemName(item *world.InvItem, count int32) string {
	name := item.Name
	if item.EnchantLvl > 0 {
		name = fmt.Sprintf("+%d %s", item.EnchantLvl, name)
	}
	if count > 1 {
		name = fmt.Sprintf("%s (%d)", name, count)
	}
	return name
}
