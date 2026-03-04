package handler

import (
	"fmt"
	"math"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// handleRefineResolve 處理火神精煉（C_PledgeContent type=13）— 分解物品為結晶。
// Java 815: C_PledgeContent case 13
// 封包格式：[D npcObjID][D itemObjID][D assistItemObjID]
//
// 流程：
//  1. 驗證 NPC 存在且在範圍內
//  2. 從背包取得物品，查火結晶表計算結晶數量
//  3. 移除原物品
//  4. 給予魔法結晶體（item 41246）
func handleRefineResolve(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	npcObjID := r.ReadD()
	itemObjID := r.ReadD()
	_ = r.ReadD() // assistItemObjID（輔助道具，暫不支援）

	// 驗證 NPC 存在且在範圍內
	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		return
	}
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx > 5 || dy > 5 {
		return
	}

	// 查找玩家背包中的物品
	item := player.Inv.FindByObjectID(itemObjID)
	if item == nil || item.Equipped {
		sendGlobalChat(sess, 9, "\\f3無法精煉該物品。")
		return
	}

	// 火結晶表未載入
	if deps.FireCrystals == nil {
		sendGlobalChat(sess, 9, "\\f3精煉系統尚未啟用。")
		return
	}

	itemInfo := deps.Items.Get(item.ItemID)
	if itemInfo == nil || itemInfo.Category == data.CategoryEtcItem {
		sendGlobalChat(sess, 9, "\\f3此物品無法精煉。")
		return
	}

	// 計算基礎 item ID（去除祝福/詛咒偏移）
	// Java: bless==0 → itemId-100000; bless==2 → itemId-200000
	lookupID := item.ItemID
	if item.Bless == 0 { // 祝福狀態
		candidateID := item.ItemID - 100000
		if ci := deps.Items.Get(candidateID); ci != nil && ci.Name == itemInfo.Name {
			lookupID = candidateID
		}
	} else if item.Bless == 2 { // 詛咒狀態
		candidateID := item.ItemID - 200000
		if ci := deps.Items.Get(candidateID); ci != nil && ci.Name == itemInfo.Name {
			lookupID = candidateID
		}
	}

	entry := deps.FireCrystals.Get(lookupID)
	if entry == nil {
		sendGlobalChat(sess, 9, "\\f3此物品無法精煉。")
		return
	}

	crystalCount := entry.GetCrystalCount(int(item.EnchantLvl), int(itemInfo.Category), itemInfo.SafeEnchant)
	if crystalCount <= 0 {
		sendGlobalChat(sess, 9, "\\f3此物品無法精煉。")
		return
	}

	// 移除原物品（武器/防具不可堆疊，移除 1 個）
	removed := player.Inv.RemoveItem(item.ObjectID, 1)
	if removed {
		sendRemoveInventoryItem(sess, item.ObjectID)
	} else {
		sendItemCountUpdate(sess, item)
	}

	// 給予魔法結晶體（item 41246）
	const crystalItemID int32 = 41246
	crystalInfo := deps.Items.Get(crystalItemID)
	if crystalInfo != nil {
		newItem := player.Inv.AddItem(crystalItemID, crystalCount, crystalInfo.Name,
			crystalInfo.InvGfx, crystalInfo.Weight, crystalInfo.Stackable, byte(crystalInfo.Bless))
		newItem.UseType = data.UseTypeToID(crystalInfo.UseType)
		sendAddItem(sess, newItem, crystalInfo)
	}

	sendWeightUpdate(sess, player)

	// 系統訊息：獲得 X 個火神結晶體
	sendGlobalChat(sess, 9, fmt.Sprintf("\\f2獲得 %d 個火神結晶體。", crystalCount))
	deps.Log.Info(fmt.Sprintf("火神精煉  角色=%s  物品=%d(+%d)  結晶=%d", player.Name, item.ItemID, item.EnchantLvl, crystalCount))
}

// handleRefineTransform 處理火神合成（C_PledgeContent type=14）— 材料合成裝備。
// Java 815: C_PledgeContent case 14
// 封包格式：[D npcObjID][H actionID][D assistItemObjID]
func handleRefineTransform(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	npcObjID := r.ReadD()
	actionID := r.ReadH()
	_ = r.ReadD() // assistItemObjID（輔助道具，暫不支援）

	// 驗證 NPC 存在且在範圍內
	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		return
	}
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx > 5 || dy > 5 {
		return
	}

	if deps.ItemMaking == nil || deps.Craft == nil {
		sendGlobalChat(sess, 9, "\\f3製作系統尚未啟用。")
		return
	}

	// 優先使用 handleCraftSelect 已儲存的配方 key（ItemBlend 確認走此路徑）
	var recipe *data.CraftRecipe
	if player.PendingCraftKey != "" && player.PendingCraftNpcID == npc.NpcID {
		recipe = deps.ItemMaking.GetByNpcAction(player.PendingCraftNpcID, player.PendingCraftKey)
		player.PendingCraftKey = ""
		player.PendingCraftNpcID = 0
		player.CraftTradeTick = 0
	}

	// 回退：嘗試透過索引查找配方（type 49 合成 UI 發送數字索引）
	if recipe == nil {
		recipe = deps.ItemMaking.GetByNpcIndex(npc.NpcID, int(actionID))
	}
	if recipe == nil {
		recipe = deps.ItemMaking.GetByNpcAction(npc.NpcID, fmt.Sprintf("%d", actionID))
	}
	if recipe == nil {
		deps.Log.Debug("火神合成：找不到配方",
			zap.Int32("npcID", npc.NpcID),
			zap.Uint16("actionID", actionID),
		)
		sendGlobalChat(sess, 9, "\\f3找不到對應的製作配方。")
		return
	}

	// 委派給 CraftSystem 執行完整的材料驗證 + 消耗 + 製作流程
	deps.Craft.ExecuteCraft(sess, player, npc, recipe, 1)
}
