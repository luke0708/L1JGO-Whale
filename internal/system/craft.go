package system

import (
	"fmt"
	"math"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// CraftSystem 負責 NPC 製作業務邏輯（材料驗證、消耗、生產物品）。
// 實作 handler.CraftManager 介面。
type CraftSystem struct {
	deps *handler.Deps
}

// NewCraftSystem 建立製作系統。
func NewCraftSystem(deps *handler.Deps) *CraftSystem {
	return &CraftSystem{deps: deps}
}

// HandleCraftEntry 製作入口：檢查 NPC 限制、計算可製作套數、顯示批量對話或直接製作。
// Java: L1NpcMakeItemAction.execute()
func (s *CraftSystem) HandleCraftEntry(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, recipe *data.CraftRecipe, action string) {
	// NPC 限制：recipe.NpcID == 0 表示任意 NPC
	if recipe.NpcID != 0 && recipe.NpcID != npc.NpcID {
		return
	}

	// 計算可製作套數
	sets := countMaterialSets(player.Inv, recipe.Materials)
	if sets <= 0 {
		// 回報第一個不足的材料（Java: msg 337 + 物品名 + 缺少數量）
		for _, mat := range recipe.Materials {
			have := countUnequippedByID(player.Inv, mat.ItemID)
			if have < mat.Amount {
				shortage := mat.Amount - have
				itemInfo := s.deps.Items.Get(mat.ItemID)
				name := fmt.Sprintf("item#%d", mat.ItemID)
				if itemInfo != nil {
					name = itemInfo.Name
				}
				handler.SendServerMessageArgs(sess, 337, name, fmt.Sprintf("%d", shortage))
				return
			}
		}
		return
	}

	// 多套材料且配方支援批量輸入 → 顯示 spinner 對話框
	if sets > 1 && recipe.AmountInputable {
		handler.SendInputAmount(sess, npc.ID, sets, action)
		player.PendingCraftAction = action
		return
	}

	// 單次製作
	s.ExecuteCraft(sess, player, npc, recipe, 1)
}

// ExecuteCraft 執行製作：驗證限制、材料、消耗、機率判定、生產物品。
// Java: L1Blend.CheckCraftItem() + L1Blend.CraftItem()
func (s *CraftSystem) ExecuteCraft(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, recipe *data.CraftRecipe, amount int32) {
	if amount <= 0 {
		return
	}

	npcObjID := int32(0)
	npcName := ""
	if npc != nil {
		npcObjID = npc.ID
		if npcInfo := s.deps.Npcs.Get(npc.NpcID); npcInfo != nil {
			npcName = npcInfo.Name
		}
	}

	// === 前置條件檢查 ===

	// 等級限制
	if recipe.RequiredLevel > 0 && int32(player.Level) < recipe.RequiredLevel {
		handler.SendGlobalChat(sess, 9, fmt.Sprintf("\\f3等級必須為 %d 以上。", recipe.RequiredLevel))
		handler.SendCloseList(sess, npcObjID)
		return
	}

	// 職業限制
	if recipe.RequiredClass > 0 {
		if !matchClass(player.ClassType, recipe.RequiredClass) {
			handler.SendGlobalChat(sess, 9, fmt.Sprintf("\\f3職業必須是 %s。", classIDToStr(recipe.RequiredClass)))
			handler.SendCloseList(sess, npcObjID)
			return
		}
	}

	// HP/MP 消耗檢查
	if recipe.HPConsume > 0 && int32(player.HP) <= recipe.HPConsume {
		handler.SendGlobalChat(sess, 9, "\\f3HP 不足。")
		handler.SendCloseList(sess, npcObjID)
		return
	}
	if recipe.MPConsume > 0 && int32(player.MP) < recipe.MPConsume {
		handler.SendGlobalChat(sess, 9, "\\f3MP 不足。")
		handler.SendCloseList(sess, npcObjID)
		return
	}

	// 材料檢查（數量 + 強化值）
	for _, mat := range recipe.Materials {
		have := countUnequippedByIDEnchant(player.Inv, mat.ItemID, mat.EnchantLvl)
		need := mat.Amount * amount
		if have < need {
			shortage := need - have
			itemInfo := s.deps.Items.Get(mat.ItemID)
			name := fmt.Sprintf("item#%d", mat.ItemID)
			if itemInfo != nil {
				name = itemInfo.Name
			}
			handler.SendServerMessageArgs(sess, 337, name, fmt.Sprintf("%d", shortage))
			handler.SendCloseList(sess, npcObjID)
			return
		}
	}

	// 背包空間檢查
	newSlots := 0
	for _, out := range recipe.Items {
		outInfo := s.deps.Items.Get(out.ItemID)
		if outInfo != nil && outInfo.Stackable {
			existing := player.Inv.FindByItemID(out.ItemID)
			if existing == nil {
				newSlots++
			}
		} else {
			newSlots += int(out.Amount) * int(amount)
		}
	}
	if recipe.BonusItemID > 0 {
		bonusInfo := s.deps.Items.Get(recipe.BonusItemID)
		if bonusInfo != nil && bonusInfo.Stackable {
			if player.Inv.FindByItemID(recipe.BonusItemID) == nil {
				newSlots++
			}
		} else {
			newSlots += int(recipe.BonusItemCount) * int(amount)
		}
	}
	if player.Inv.Size()+newSlots > world.MaxInventorySize {
		handler.SendServerMessage(sess, 263)
		handler.SendCloseList(sess, npcObjID)
		return
	}

	// 負重檢查
	var addWeight int32
	for _, out := range recipe.Items {
		if outInfo := s.deps.Items.Get(out.ItemID); outInfo != nil {
			addWeight += outInfo.Weight * out.Amount * amount
		}
	}
	maxW := world.PlayerMaxWeight(player)
	if player.Inv.IsOverWeight(addWeight, maxW) {
		handler.SendServerMessage(sess, 82)
		handler.SendCloseList(sess, npcObjID)
		return
	}

	// === 消耗材料 ===
	for _, mat := range recipe.Materials {
		remaining := mat.Amount * amount
		for remaining > 0 {
			slot := findUnequippedByIDEnchant(player.Inv, mat.ItemID, mat.EnchantLvl)
			if slot == nil {
				break
			}
			take := remaining
			if take > slot.Count {
				take = slot.Count
			}
			removed := player.Inv.RemoveItem(slot.ObjectID, take)
			if removed {
				handler.SendRemoveInventoryItem(sess, slot.ObjectID)
			} else {
				handler.SendItemCountUpdate(sess, slot)
			}
			remaining -= take
		}
	}

	// 消耗 HP/MP
	if recipe.HPConsume > 0 {
		player.HP -= int32(recipe.HPConsume)
		if player.HP < 1 {
			player.HP = 1
		}
		handler.SendHpUpdate(sess, player)
	}
	if recipe.MPConsume > 0 {
		player.MP -= int32(recipe.MPConsume)
		if player.MP < 0 {
			player.MP = 0
		}
		handler.SendMpUpdate(sess, player)
	}

	// === 機率判定 + 製作 ===
	successRate := recipe.SuccessRate
	if successRate <= 0 {
		successRate = 100 // 0 = 100% 成功
	}

	if recipe.AllInOnce || amount == 1 {
		// 一次判定模式（或單件製作）
		if successRate >= 100 || world.RandInt(1000) < int(successRate)*10 {
			s.produceItems(sess, player, npc, recipe, amount, npcName)
		} else {
			s.produceFailed(sess, player, recipe, npcObjID)
		}
	} else {
		// 逐件判定模式
		var successCount, failCount int32
		for i := int32(0); i < amount; i++ {
			if successRate >= 100 || world.RandInt(1000) < int(successRate)*10 {
				successCount++
			} else {
				failCount++
			}
		}
		if successCount > 0 {
			s.produceItems(sess, player, npc, recipe, successCount, npcName)
		}
		if failCount > 0 {
			s.produceResidueItems(sess, player, recipe, failCount)
		}
	}

	handler.SendWeightUpdate(sess, player)
	handler.SendCloseList(sess, npcObjID)

	s.deps.Log.Info(fmt.Sprintf("製作完成  角色=%s  配方=%s  數量=%d  NPC=%s",
		player.Name, recipe.Action, amount, npcName))
}

// produceItems 生產成功的成品。
func (s *CraftSystem) produceItems(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, recipe *data.CraftRecipe, amount int32, npcName string) {
	// 成品
	for _, out := range recipe.Items {
		outInfo := s.deps.Items.Get(out.ItemID)
		if outInfo == nil {
			continue
		}
		totalCount := out.Amount * amount

		if outInfo.Stackable {
			item := player.Inv.AddItem(out.ItemID, totalCount, outInfo.Name,
				outInfo.InvGfx, outInfo.Weight, true, byte(outInfo.Bless))
			item.UseType = data.UseTypeToID(outInfo.UseType)
			if out.EnchantLvl > 0 {
				item.EnchantLvl = int8(out.EnchantLvl)
			}
			handler.SendAddItem(sess, item, outInfo)
		} else {
			for i := int32(0); i < totalCount; i++ {
				bless := byte(outInfo.Bless)
				if out.Bless > 0 {
					bless = byte(out.Bless)
				}
				item := player.Inv.AddItem(out.ItemID, 1, outInfo.Name,
					outInfo.InvGfx, outInfo.Weight, false, bless)
				item.UseType = data.UseTypeToID(outInfo.UseType)
				if out.EnchantLvl > 0 {
					item.EnchantLvl = int8(out.EnchantLvl)
				}
				item.Identified = true
				handler.SendAddItem(sess, item, outInfo)
			}
		}

		if npcName != "" {
			handler.SendServerMessageArgs(sess, 143, npcName, outInfo.Name)
		}
	}

	// 加成物品（成功時額外獎勵）
	if recipe.BonusItemID > 0 && recipe.BonusItemCount > 0 {
		bonusInfo := s.deps.Items.Get(recipe.BonusItemID)
		if bonusInfo != nil {
			totalBonus := recipe.BonusItemCount * amount
			if bonusInfo.Stackable {
				item := player.Inv.AddItem(recipe.BonusItemID, totalBonus, bonusInfo.Name,
					bonusInfo.InvGfx, bonusInfo.Weight, true, byte(bonusInfo.Bless))
				item.UseType = data.UseTypeToID(bonusInfo.UseType)
				handler.SendAddItem(sess, item, bonusInfo)
			} else {
				for i := int32(0); i < totalBonus; i++ {
					item := player.Inv.AddItem(recipe.BonusItemID, 1, bonusInfo.Name,
						bonusInfo.InvGfx, bonusInfo.Weight, false, byte(bonusInfo.Bless))
					item.UseType = data.UseTypeToID(bonusInfo.UseType)
					handler.SendAddItem(sess, item, bonusInfo)
				}
			}
			if npcName != "" {
				handler.SendServerMessageArgs(sess, 143, npcName, bonusInfo.Name)
			}
		}
	}

	// 全服廣播（Java: S_GreenMessage）
	if recipe.Broadcast {
		resultName := ""
		if len(recipe.Items) > 0 {
			if info := s.deps.Items.Get(recipe.Items[0].ItemID); info != nil {
				resultName = info.Name
			}
		}
		msg := fmt.Sprintf("%s 成功製作了 %s！", player.Name, resultName)
		broadcastData := handler.BuildGreenMessage(msg)
		s.deps.World.AllPlayers(func(p *world.PlayerInfo) {
			p.Session.Send(broadcastData)
		})
	}
}

// produceFailed 處理製作失敗（生成殘留物品）。
func (s *CraftSystem) produceFailed(sess *net.Session, player *world.PlayerInfo, recipe *data.CraftRecipe, npcObjID int32) {
	handler.SendGlobalChat(sess, 9, "\\f3道具製造失敗了。")
	s.produceResidueItems(sess, player, recipe, 1)
}

// produceResidueItems 生成失敗殘留物品。
func (s *CraftSystem) produceResidueItems(sess *net.Session, player *world.PlayerInfo, recipe *data.CraftRecipe, failCount int32) {
	if recipe.ResidueItemID <= 0 || recipe.ResidueItemCount <= 0 {
		return
	}
	resInfo := s.deps.Items.Get(recipe.ResidueItemID)
	if resInfo == nil {
		return
	}
	totalRes := recipe.ResidueItemCount * failCount
	if resInfo.Stackable {
		item := player.Inv.AddItem(recipe.ResidueItemID, totalRes, resInfo.Name,
			resInfo.InvGfx, resInfo.Weight, true, byte(resInfo.Bless))
		item.UseType = data.UseTypeToID(resInfo.UseType)
		handler.SendAddItem(sess, item, resInfo)
	} else {
		for i := int32(0); i < totalRes; i++ {
			item := player.Inv.AddItem(recipe.ResidueItemID, 1, resInfo.Name,
				resInfo.InvGfx, resInfo.Weight, false, byte(resInfo.Bless))
			item.UseType = data.UseTypeToID(resInfo.UseType)
			handler.SendAddItem(sess, item, resInfo)
		}
	}
}

// === 私有輔助函式 ===

// countMaterialSets 計算玩家可提供幾套完整材料。
func countMaterialSets(inv *world.Inventory, materials []data.CraftMaterial) int32 {
	if len(materials) == 0 {
		return 0
	}
	var minSets int32 = math.MaxInt32
	for _, mat := range materials {
		have := countUnequippedByID(inv, mat.ItemID)
		if mat.Amount <= 0 {
			continue
		}
		sets := have / mat.Amount
		if sets < minSets {
			minSets = sets
		}
	}
	if minSets == math.MaxInt32 {
		return 0
	}
	return minSets
}

// countUnequippedByID 計算未裝備的指定物品總數量。
func countUnequippedByID(inv *world.Inventory, itemID int32) int32 {
	var total int32
	for _, it := range inv.Items {
		if it.ItemID == itemID && !it.Equipped {
			total += it.Count
		}
	}
	return total
}

// countUnequippedByIDEnchant 計算未裝備且強化值 >= 要求的指定物品總數量。
func countUnequippedByIDEnchant(inv *world.Inventory, itemID int32, reqEnchant int32) int32 {
	var total int32
	for _, it := range inv.Items {
		if it.ItemID == itemID && !it.Equipped && int32(it.EnchantLvl) >= reqEnchant {
			total += it.Count
		}
	}
	return total
}

// findUnequippedByID 找到第一個未裝備的指定物品。
func findUnequippedByID(inv *world.Inventory, itemID int32) *world.InvItem {
	for _, it := range inv.Items {
		if it.ItemID == itemID && !it.Equipped {
			return it
		}
	}
	return nil
}

// findUnequippedByIDEnchant 找到第一個未裝備且強化值 >= 要求的指定物品。
func findUnequippedByIDEnchant(inv *world.Inventory, itemID int32, reqEnchant int32) *world.InvItem {
	for _, it := range inv.Items {
		if it.ItemID == itemID && !it.Equipped && int32(it.EnchantLvl) >= reqEnchant {
			return it
		}
	}
	return nil
}

// matchClass 檢查玩家職業是否符合要求。
// classType: 0=王族, 1=騎士, 2=法師, 3=妖精, 4=黑暗妖精, 5=龍騎士, 6=幻術師, 7=戰士
// requiredClass: 1=王族, 2=騎士, 3=法師, 4=妖精, 5=黑妖, 6=龍騎, 7=幻術師, 8=戰士
func matchClass(classType int16, requiredClass int32) bool {
	return int32(classType)+1 == requiredClass
}

// classIDToStr 將配方職業 ID 轉為顯示名稱（與 handler 的 classIDToName 相同）。
func classIDToStr(classID int32) string {
	switch classID {
	case 1:
		return "王族"
	case 2:
		return "騎士"
	case 3:
		return "法師"
	case 4:
		return "妖精"
	case 5:
		return "黑暗妖精"
	case 6:
		return "龍騎士"
	case 7:
		return "幻術師"
	case 8:
		return "戰士"
	default:
		return ""
	}
}
