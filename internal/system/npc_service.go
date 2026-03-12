package system

import (
	"math/rand"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// NpcServiceSystem 處理 NPC 服務邏輯（治療、附魔、變身、傳送、物品升級）。
// 實作 handler.NpcServiceManager 介面。
type NpcServiceSystem struct {
	deps *handler.Deps
}

// NewNpcServiceSystem 建立 NPC 服務系統。
func NewNpcServiceSystem(deps *handler.Deps) *NpcServiceSystem {
	return &NpcServiceSystem{deps: deps}
}

// NpcFullHeal 處理 NPC 完整治療。
func (s *NpcServiceSystem) NpcFullHeal(sess *net.Session, player *world.PlayerInfo, npcID int32) {
	if healer := s.deps.NpcServices.GetHealer(npcID); healer != nil {
		s.execHeal(sess, player, healer)
		return
	}
	// 未在 YAML 中定義的通用 NPC 完整治療
	player.HP = player.MaxHP
	player.MP = player.MaxMP
	player.Dirty = true
	handler.SendHpUpdate(sess, player)
	handler.SendMpUpdate(sess, player)
	handler.SendServerMessage(sess, 77) // "你覺得舒服多了"
	handler.BroadcastEffectOnPlayer(sess, player, 830, s.deps)
	handler.UpdatePartyMiniHP(player, s.deps)
}

// execHeal 依 YAML 定義的治療參數執行治療。
func (s *NpcServiceSystem) execHeal(sess *net.Session, player *world.PlayerInfo, h *data.HealerDef) {
	// 扣費
	if h.Cost > 0 {
		if !s.consumeAdena(sess, player, h.Cost) {
			handler.SendServerMessageArgs(sess, 337, "$4") // "金幣不足"
			return
		}
	}

	switch h.HealType {
	case "random":
		healRange := h.HealMax - h.HealMin + 1
		healAmt := int32(rand.Intn(healRange) + h.HealMin)
		if player.HP < player.MaxHP {
			player.HP += healAmt
			if player.HP > player.MaxHP {
				player.HP = player.MaxHP
			}
		}
		player.Dirty = true
		handler.SendHpUpdate(sess, player)
	case "full":
		if h.Target == "hp_mp" || h.Target == "hp" {
			player.HP = player.MaxHP
			handler.SendHpUpdate(sess, player)
		}
		if h.Target == "hp_mp" || h.Target == "mp" {
			player.MP = player.MaxMP
			handler.SendMpUpdate(sess, player)
		}
		player.Dirty = true
		handler.UpdatePartyMiniHP(player, s.deps)
	}

	handler.SendServerMessage(sess, h.MsgID)
	handler.BroadcastEffectOnPlayer(sess, player, h.Gfx, s.deps)
}

// NpcWeaponEnchant 處理 NPC 武器附魔。
func (s *NpcServiceSystem) NpcWeaponEnchant(sess *net.Session, player *world.PlayerInfo) {
	we := s.deps.NpcServices.WeaponEnchant()
	weapon := player.Equip.Weapon()
	if weapon == nil {
		handler.SendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	if weapon.DmgByMagic > 0 && weapon.DmgMagicExpiry > 0 {
		weapon.DmgByMagic = 0
		weapon.DmgMagicExpiry = 0
	}

	weapon.DmgByMagic = we.DmgBonus
	weapon.DmgMagicExpiry = we.DurationSec * 5
	player.Dirty = true

	handler.RecalcEquipStats(sess, player, s.deps)
	handler.BroadcastEffectOnPlayer(sess, player, we.Gfx, s.deps)
	handler.SendServerMessageArgs(sess, 161, weapon.Name, "$245", "$247")
}

// NpcArmorEnchant 處理 NPC 防具附魔。
func (s *NpcServiceSystem) NpcArmorEnchant(sess *net.Session, player *world.PlayerInfo) {
	ae := s.deps.NpcServices.ArmorEnchant()
	armor := player.Equip.Get(world.SlotArmor)
	if armor == nil {
		handler.SendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	if armor.AcByMagic > 0 && armor.AcMagicExpiry > 0 {
		armor.AcByMagic = 0
		armor.AcMagicExpiry = 0
	}

	armor.AcByMagic = ae.AcBonus
	armor.AcMagicExpiry = ae.DurationSec * 5
	player.Dirty = true

	handler.RecalcEquipStats(sess, player, s.deps)
	handler.BroadcastEffectOnPlayer(sess, player, ae.Gfx, s.deps)
	handler.SendServerMessageArgs(sess, 161, armor.Name, "$245", "$247")
}

// NpcPoly 處理 NPC 變身服務（扣費 + 委派 PolymorphSystem）。
func (s *NpcServiceSystem) NpcPoly(sess *net.Session, player *world.PlayerInfo, polyID int32) {
	poly := s.deps.NpcServices.Polymorph()
	if !s.consumeAdena(sess, player, poly.Cost) {
		handler.SendServerMessageArgs(sess, 337, "$4") // "金幣不足"
		return
	}
	if s.deps.Polymorph != nil {
		s.deps.Polymorph.DoPoly(player, polyID, poly.DurationSec, data.PolyCauseNPC)
	}
}

// NpcTeleportWithCost 處理 NPC 傳送（扣費 + 出發特效 + 延遲傳送）。
func (s *NpcServiceSystem) NpcTeleportWithCost(sess *net.Session, player *world.PlayerInfo, dest *data.TeleportDest, objID int32) {
	// 扣費
	if dest.Price > 0 {
		currentGold := player.Inv.GetAdena()
		if currentGold < dest.Price {
			handler.SendServerMessage(sess, 189) // "金幣不足"
			return
		}
		if !s.consumeAdena(sess, player, dest.Price) {
			handler.SendServerMessage(sess, 189)
			return
		}
	}

	// 出發特效 + 延遲 2 tick（400ms）傳送
	handler.SendEffectOnPlayer(sess, player.CharID, 169)
	nearby := s.deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, sess.ID)
	for _, viewer := range nearby {
		handler.SendEffectOnPlayer(viewer.Session, player.CharID, 169)
	}
	player.ScrollTPTick = 2
	player.ScrollTPX = dest.X
	player.ScrollTPY = dest.Y
	player.ScrollTPMap = dest.MapID
}

// NpcUpgrade 處理物品升級合成。
func (s *NpcServiceSystem) NpcUpgrade(sess *net.Session, player *world.PlayerInfo, upg *data.ItemUpgrade) {
	// 1. 驗證主物品
	mainItem := player.Inv.FindByItemID(upg.MainItemID)
	if mainItem == nil || mainItem.Count < upg.MainItemCount {
		handler.SendSystemMessage(sess, "缺少必要的主要材料。")
		return
	}

	// 2. 驗證需求材料
	for i, needID := range upg.NeedItemIDs {
		if i >= len(upg.NeedCounts) {
			break
		}
		needItem := player.Inv.FindByItemID(needID)
		if needItem == nil || needItem.Count < upg.NeedCounts[i] {
			handler.SendSystemMessage(sess, "缺少必要的材料。")
			return
		}
	}

	// 3. 計算加成材料機率
	bonusChance := 0
	plusItems := make([]*world.InvItem, 0)
	for i, plusID := range upg.PlusItemIDs {
		if i >= len(upg.PlusCounts) || i >= len(upg.PlusAddChance) {
			break
		}
		pi := player.Inv.FindByItemID(plusID)
		if pi != nil && pi.Count >= upg.PlusCounts[i] {
			bonusChance += upg.PlusAddChance[i]
			plusItems = append(plusItems, pi)
		}
	}

	// 4. 消耗主物品
	removed := player.Inv.RemoveItem(mainItem.ObjectID, upg.MainItemCount)
	if removed {
		handler.SendRemoveInventoryItem(sess, mainItem.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, mainItem)
	}

	// 5. 消耗需求材料
	for i, needID := range upg.NeedItemIDs {
		if i >= len(upg.NeedCounts) {
			break
		}
		needItem := player.Inv.FindByItemID(needID)
		if needItem == nil {
			continue
		}
		r := player.Inv.RemoveItem(needItem.ObjectID, upg.NeedCounts[i])
		if r {
			handler.SendRemoveInventoryItem(sess, needItem.ObjectID)
		} else {
			handler.SendItemCountUpdate(sess, needItem)
		}
	}

	// 6. 消耗加成材料
	for i, pi := range plusItems {
		if i >= len(upg.PlusCounts) {
			break
		}
		r := player.Inv.RemoveItem(pi.ObjectID, upg.PlusCounts[i])
		if r {
			handler.SendRemoveInventoryItem(sess, pi.ObjectID)
		} else {
			handler.SendItemCountUpdate(sess, pi)
		}
	}

	player.Dirty = true

	// 7. 機率判定
	totalChance := upg.UpgradeChance + bonusChance
	roll := rand.Intn(100)
	if roll < totalChance {
		s.upgradeSuccess(sess, player, upg)
	} else {
		if upg.DeleteChance > 0 && rand.Intn(100) < upg.DeleteChance {
			s.upgradeDelete(sess, upg)
		} else {
			s.upgradeFailure(sess, upg)
		}
	}
}

// upgradeSuccess 升級成功：給予新物品 + 顯示成功 HTML。
func (s *NpcServiceSystem) upgradeSuccess(sess *net.Session, player *world.PlayerInfo, upg *data.ItemUpgrade) {
	if upg.NewItemID > 0 {
		itemInfo := s.deps.Items.Get(upg.NewItemID)
		if itemInfo != nil {
			stackable := itemInfo.Stackable || upg.NewItemID == world.AdenaItemID
			existing := player.Inv.FindByItemID(upg.NewItemID)
			wasExisting := existing != nil && stackable

			invItem := player.Inv.AddItem(upg.NewItemID, 1, itemInfo.Name, itemInfo.InvGfx,
				itemInfo.Weight, stackable, byte(itemInfo.Bless))
			invItem.UseType = itemInfo.UseTypeID
			invItem.Identified = true

			if wasExisting {
				handler.SendItemCountUpdate(sess, invItem)
			} else {
				handler.SendAddItem(sess, invItem, itemInfo)
			}
			handler.SendWeightUpdate(sess, player)
		}
	}

	if upg.SuccessHTML != "" {
		handler.SendHypertext(sess, 0, upg.SuccessHTML)
	} else {
		handler.SendSystemMessage(sess, "升級成功！")
	}
}

// upgradeFailure 升級失敗（不刪除）：顯示失敗 HTML。
func (s *NpcServiceSystem) upgradeFailure(sess *net.Session, upg *data.ItemUpgrade) {
	if upg.FailureHTML != "" {
		handler.SendHypertext(sess, 0, upg.FailureHTML)
	} else {
		handler.SendSystemMessage(sess, "升級失敗，材料已消耗。")
	}
}

// upgradeDelete 升級大失敗（刪除原物品）：顯示刪除 HTML。
func (s *NpcServiceSystem) upgradeDelete(sess *net.Session, upg *data.ItemUpgrade) {
	if upg.DeleteHTML != "" {
		handler.SendHypertext(sess, 0, upg.DeleteHTML)
	} else {
		handler.SendSystemMessage(sess, "升級失敗，材料已被破壞。")
	}
}

// ConsumeAdena 扣除玩家金幣並發送更新封包。成功回傳 true，不足回傳 false。
func (s *NpcServiceSystem) ConsumeAdena(sess *net.Session, player *world.PlayerInfo, amount int32) bool {
	return s.consumeAdena(sess, player, amount)
}

// consumeAdena 內部扣費邏輯。
func (s *NpcServiceSystem) consumeAdena(sess *net.Session, player *world.PlayerInfo, amount int32) bool {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena == nil || adena.Count < amount {
		return false
	}
	adena.Count -= amount
	if adena.Count <= 0 {
		player.Inv.RemoveItem(adena.ObjectID, 0)
		handler.SendRemoveInventoryItem(sess, adena.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, adena)
	}
	handler.SendWeightUpdate(sess, player)
	player.Dirty = true
	return true
}

// RepairWeapon 處理武器修理（扣費 + 修復耐久度）。
func (s *NpcServiceSystem) RepairWeapon(sess *net.Session, player *world.PlayerInfo, weapon *world.InvItem, cost int32) bool {
	if !s.consumeAdena(sess, player, cost) {
		return false
	}
	weapon.Durability = 0
	player.Dirty = true
	handler.SendItemCountUpdate(sess, weapon)
	s.deps.Log.Debug("武器修理完成",
		zap.String("player", player.Name),
		zap.String("weapon", weapon.Name),
		zap.Int32("cost", cost),
	)
	return true
}

// ConsumeItem 消耗背包物品（移除 + 發送更新 + 標記 dirty）。
func (s *NpcServiceSystem) ConsumeItem(sess *net.Session, player *world.PlayerInfo, objectID int32, count int32) bool {
	item := player.Inv.FindByObjectID(objectID)
	if item == nil {
		return false
	}
	removed := player.Inv.RemoveItem(objectID, count)
	if removed {
		handler.SendRemoveInventoryItem(sess, objectID)
	} else {
		handler.SendItemCountUpdate(sess, item)
	}
	handler.SendWeightUpdate(sess, player)
	player.Dirty = true
	return true
}

// Refine 火神精煉分解（移除裝備 + 給予結晶體）。
func (s *NpcServiceSystem) Refine(sess *net.Session, player *world.PlayerInfo, item *world.InvItem, crystalItemID int32, crystalCount int32) {
	// 移除原物品（item 為 nil 時表示已由呼叫方移除，僅給予結晶體）
	if item != nil {
		removed := player.Inv.RemoveItem(item.ObjectID, 1)
		if removed {
			handler.SendRemoveInventoryItem(sess, item.ObjectID)
		} else {
			handler.SendItemCountUpdate(sess, item)
		}
	}

	// 給予結晶體
	crystalInfo := s.deps.Items.Get(crystalItemID)
	if crystalInfo != nil {
		existing := player.Inv.FindByItemID(crystalItemID)
		wasExisting := existing != nil && crystalInfo.Stackable

		newItem := player.Inv.AddItem(crystalItemID, crystalCount, crystalInfo.Name,
			crystalInfo.InvGfx, crystalInfo.Weight, crystalInfo.Stackable, byte(crystalInfo.Bless))
		newItem.UseType = data.UseTypeToID(crystalInfo.UseType)

		if wasExisting {
			handler.SendItemCountUpdate(sess, newItem)
		} else {
			handler.SendAddItem(sess, newItem, crystalInfo)
		}
	}

	handler.SendWeightUpdate(sess, player)
	player.Dirty = true
}
