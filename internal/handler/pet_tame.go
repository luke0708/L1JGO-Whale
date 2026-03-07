package handler

import (
	"log"
	"math"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
)

// HandleGiveItem processes C_GIVE (opcode 45).
// Java: C_GiveItem — 給予 NPC / 寵物 / 召喚獸物品。
// 路由：馴服、寵物裝備、寵物進化、藥水自動使用、一般物品消化。
func HandleGiveItem(sess *net.Session, r *packet.Reader, deps *Deps) {
	targetID := r.ReadD()
	_ = r.ReadH() // x（封包座標，伺服器使用實體座標）
	_ = r.ReadH() // y
	itemObjID := r.ReadD()
	count := r.ReadD()
	if count <= 0 {
		count = 1
	}

	log.Printf("[GiveItem] targetID=%d itemObjID=%d count=%d sessID=%d", targetID, itemObjID, count, sess.ID)

	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Dead {
		log.Printf("[GiveItem] 玩家未找到或已死 sessID=%d", sess.ID)
		return
	}

	invItem := player.Inv.FindByObjectID(itemObjID)
	if invItem == nil || invItem.Count <= 0 {
		log.Printf("[GiveItem] 物品未找到 itemObjID=%d", itemObjID)
		return
	}

	log.Printf("[GiveItem] 物品: itemID=%d name=%s count=%d bless=%d equipped=%v",
		invItem.ItemID, invItem.Name, invItem.Count, invItem.Bless, invItem.Equipped)

	// 已裝備的物品不可給予（Java: S_ServerMessage 141）
	if invItem.Equipped {
		sendServerMessage(sess, 141) // 裝備使用中的東西不可以給予他人
		return
	}

	// bless >= 128 不可給予（Java: item.getBless() >= 128）
	if invItem.Bless >= 128 {
		sendServerMessage(sess, 210) // 不可轉移
		return
	}

	// 不可交易的物品不可給予（Java: item.getItem().isTradable()）
	if deps.Items != nil {
		itemInfo := deps.Items.Get(invItem.ItemID)
		if itemInfo != nil && !itemInfo.Tradeable {
			sendServerMessage(sess, 210) // 不可轉移
			return
		}
	}

	// 寵物項圈保護 — 不可給出正在使用的寵物項圈（Java: pet collar check）
	if isPetCollar(invItem.ItemID) {
		for _, pet := range deps.World.GetPetsByOwner(player.CharID) {
			if pet.ItemObjID == invItem.ObjectID {
				sendServerMessage(sess, 210) // 不可轉移
				return
			}
		}
	}

	// 目標是自己的寵物 → 委派給 PetLife
	if pet := deps.World.GetPet(targetID); pet != nil {
		log.Printf("[GiveItem] 目標是寵物 petID=%d ownerCharID=%d", pet.ID, pet.OwnerCharID)
		// 距離檢查（3 格內）— Java 統一在 tradeItem 前檢查
		dx := int32(math.Abs(float64(player.X - pet.X)))
		dy := int32(math.Abs(float64(player.Y - pet.Y)))
		if dx >= 3 || dy >= 3 {
			sendServerMessage(sess, 142) // 太遠了
			return
		}
		if pet.OwnerCharID == player.CharID && deps.PetLife != nil {
			deps.PetLife.GiveToPet(sess, player, pet, invItem)
		}
		return
	}

	// 目標是野生 NPC → 馴服嘗試
	npc := deps.World.GetNpc(targetID)
	if npc == nil || npc.Dead {
		log.Printf("[GiveItem] NPC 未找到或已死 targetID=%d (npc=%v)", targetID, npc)
		return
	}

	log.Printf("[GiveItem] 目標NPC: npcID=%d name=%s HP=%d/%d mapID=%d",
		npc.NpcID, npc.Name, npc.HP, npc.MaxHP, npc.MapID)

	// 距離檢查（3 格內）
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx >= 3 || dy >= 3 {
		sendServerMessage(sess, 142) // 太遠了
		return
	}

	// 檢查 NPC + 物品組合是否為馴服嘗試
	petType := deps.PetTypes.Get(npc.NpcID)
	if petType == nil {
		log.Printf("[GiveItem] PetType 未找到 npcID=%d — 非可馴服 NPC", npc.NpcID)
		return
	}
	log.Printf("[GiveItem] PetType: CanTame=%v TamingItemID=%d invItemID=%d match=%v",
		petType.CanTame(), petType.TamingItemID, invItem.ItemID, invItem.ItemID == petType.TamingItemID)

	if petType.CanTame() && invItem.ItemID == petType.TamingItemID {
		// 消耗馴服物品
		deps.NpcSvc.ConsumeItem(sess, player, itemObjID, 1)

		if deps.PetLife != nil {
			deps.PetLife.TameNpc(sess, player, npc)
		} else {
			log.Printf("[GiveItem] 錯誤: deps.PetLife 為 nil！")
		}
	} else {
		log.Printf("[GiveItem] 非馴服物品組合，忽略")
	}
}
