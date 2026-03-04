package handler

import (
	"fmt"
	"math"
	"math/rand"
	"strings"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// HandleNpcAction processes C_HACTION (opcode 125) — player clicks a button in NPC dialog.
// Also handles S_Message_YN (yes/no dialog) responses — client sends objectID=yesNoCount.
// The action string determines what to do: "buy", "sell", "teleportURL", etc.
func HandleNpcAction(sess *net.Session, r *packet.Reader, deps *Deps) {
	objID := r.ReadD()
	action := r.ReadS()

	deps.Log.Debug("C_NpcAction",
		zap.Int32("objID", objID),
		zap.String("action", action),
	)

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	// Clear pending state — any new NPC interaction overrides
	player.PendingCraftAction = ""
	player.FireSmithNpcObjID = 0

	// --- Summon ring selection: numeric string response from "summonlist" dialog ---
	// Java: L1ActionPc.java checks cmd.matches("[0-9]+") && isSummonMonster().
	if player.SummonSelectionMode && isNumericString(action) {
		HandleSummonRingSelection(sess, player, action, deps)
		return
	}

	// --- Companion entity control (summon/pet before NPC lookup) ---
	if sum := deps.World.GetSummon(objID); sum != nil {
		if sum.OwnerCharID == player.CharID {
			handleSummonAction(sess, player, sum, strings.ToLower(action), deps)
		}
		return
	}
	if pet := deps.World.GetPet(objID); pet != nil {
		if pet.OwnerCharID == player.CharID && deps.PetLife != nil {
			deps.PetLife.HandlePetAction(sess, player, pet, strings.ToLower(action))
		}
		return
	}

	npc := deps.World.GetNpc(objID)
	if npc == nil {
		// Not an NPC — check for S_Message_YN (yes/no dialog) response
		if player.PendingYesNoType != 0 {
			lAction := strings.ToLower(action)
			accepted := lAction != "no" && lAction != "n"
			handleYesNoResponse(sess, player, accepted, deps)
		}
		return
	}
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx > 5 || dy > 5 {
		return
	}

	lowerAction := strings.ToLower(action)

	// Auto-cancel trade when interacting with NPC
	cancelTradeIfActive(player, deps)

	// Paginated teleporter NPC (e.g., NPC 91053): route all actions to paged handler
	if deps.TeleportPages != nil && deps.TeleportPages.IsPageTeleportNpc(npc.NpcID) {
		handlePagedTeleportAction(sess, player, npc, action, deps)
		return
	}

	switch lowerAction {
	case "buy":
		handleShopBuy(sess, npc.NpcID, objID, deps)
	case "sell":
		handleShopSell(sess, npc.NpcID, objID, deps)
	case "buyskill":
		openSpellShop(sess, deps)
	case "teleporturl", "teleporturla", "teleporturlb", "teleporturlc",
		"teleporturld", "teleporturle", "teleporturlf", "teleporturlg",
		"teleporturlh", "teleporturli", "teleporturlj", "teleporturlk":
		handleTeleportURLGeneric(sess, npc.NpcID, objID, action, deps)

	// Warehouse — 個人帳號倉庫
	case "retrieve":
		deps.Warehouse.OpenWarehouse(sess, player, objID, WhTypePersonal)
	case "deposit":
		deps.Warehouse.OpenWarehouseDeposit(sess, player, objID, WhTypePersonal)

	// Warehouse — 角色專屬倉庫（Java: retrieve-char → S_RetrieveChaList type=18）
	case "retrieve-char":
		deps.Warehouse.OpenWarehouse(sess, player, objID, WhTypeCharacter)

	// Warehouse — 精靈倉庫
	case "retrieve-elven":
		deps.Warehouse.OpenWarehouse(sess, player, objID, WhTypeElf)
	case "deposit-elven":
		deps.Warehouse.OpenWarehouseDeposit(sess, player, objID, WhTypeElf)

	// Warehouse — 血盟倉庫（含權限驗證 + 單人鎖定）
	case "retrieve-pledge":
		deps.Warehouse.OpenClanWarehouse(sess, player, objID)
	case "deposit-pledge":
		deps.Warehouse.OpenClanWarehouse(sess, player, objID) // 同 retrieve，客戶端內建 tab 處理
	case "history":
		// 血盟倉庫歷史記錄（Java: S_PledgeWarehouseHistory）
		if player.ClanID > 0 {
			deps.Warehouse.SendClanWarehouseHistory(sess, player.ClanID)
		}

	// EXP recovery / PK redemption (stub)
	case "exp":
		sendHypertext(sess, objID, "expr")
	case "pk":
		sendHypertext(sess, objID, "pkr")

	// ---------- NPC Services (data-driven from npc_services.yaml) ----------

	case "haste":
		handleNpcHaste(sess, player, npc, deps)
	case "0":
		handleNpcActionZero(sess, player, npc, objID, deps)
	case "fullheal":
		handleNpcFullHeal(sess, player, npc, deps)
	case "encw":
		handleNpcWeaponEnchant(sess, player, deps)
	case "enca":
		handleNpcArmorEnchant(sess, player, deps)

	// ---------- 火神精煉系統 ----------
	// type 48/49 拖放 UI 在 3.80C 無法使用。

	case "itemresolve":
		// 火神精煉（商店模式）— 和 request firecrystal 相同機制
		sendFireSmithSellList(sess, player, npc, deps)
	case "itemtransform":
		// 火神製作 — ItemBlend 模板瀏覽配方 → confirm craft 開啟交易視窗 → 確認製作
		sendCraftItemBlend(sess, player, npc, deps, 0)
	case "request firecrystal":
		// 火神熔煉（商店賣出格式）— Java: Npc_FireSmith → S_ShopBuyListFireSmith
		sendFireSmithSellList(sess, player, npc, deps)

	// ---------- 火神工匠系統（Java: L1Blend / 道具製造系統DB化） ----------

	case "request craft":
		handleRequestCraft(sess, player, npc, deps)
	case "confirm craft":
		handleConfirmCraft(sess, player, npc, deps)
	case "cancel craft":
		// 循環瀏覽下一個配方（ItemBlend 模板用）
		if player.PendingCraftNpcID != 0 && deps.ItemMaking != nil {
			nextIdx := player.PendingCraftIndex + 1
			recipes := deps.ItemMaking.GetByNpcID(player.PendingCraftNpcID)
			if nextIdx < len(recipes) {
				sendCraftItemBlend(sess, player, npc, deps, nextIdx)
			} else {
				// 已到最後一個配方，回到第一個
				sendCraftItemBlend(sess, player, npc, deps, 0)
			}
		} else {
			player.PendingCraftKey = ""
			player.PendingCraftNpcID = 0
			player.PendingCraftIndex = 0
			player.CraftTradeTick = 0
		}

	// "ent" 動作 — 多個 NPC 共用，依 NPC ID 分派
	// Java: C_NPCAction.java 對 "ent" 按 npcId 做 if/else
	case "ent":
		switch npc.NpcID {
		case 80085: // 幽靈之家管理人杜烏 → 鬼屋副本
			enterHauntedHouse(sess, player, deps)
		default: // NPC 71264 回憶蠟燭嚮導等 → 角色重置
			StartCharReset(sess, player, deps)
		}

	// Close dialog (empty string or explicit close)
	case "":
		// Do nothing — dialog closes

	default:
		// ---------- NPC 專屬動作（依 NPC ID 分派） ----------
		if npc.NpcID == 81445 { // 欄位開放專家 史奈普
			handleSlotNpc(sess, player, objID, lowerAction, deps)
			return
		}

		// Check teleport destinations (handles "teleport xxx" and other
		// action names like "Strange21", "goto battle ring", "a"/"b"/etc.)
		if deps.Teleports.Get(npc.NpcID, action) != nil {
			handleTeleport(sess, player, npc.NpcID, action, deps)
			return
		}

		// Check if this is a polymorph NPC form (data-driven from npc_services.yaml)
		if polyID := deps.NpcServices.GetPolyForm(lowerAction); polyID > 0 {
			handleNpcPoly(sess, player, polyID, deps)
			return
		}

		// 火神系統配方（NPC 專屬，action = A-Z, a1-a17）
		// Java: craftkey = npcid + action → L1BlendTable.getTemplate(craftkey) → ShowCraftHtml
		if deps.ItemMaking != nil && deps.Craft != nil {
			if recipe := deps.ItemMaking.GetByNpcAction(npc.NpcID, action); recipe != nil {
				handleCraftSelect(sess, player, npc, recipe, deps)
				return
			}
			// 簡易配方（無 NPC 綁定）：直接執行
			if recipe := deps.ItemMaking.Get(action); recipe != nil {
				deps.Craft.HandleCraftEntry(sess, player, npc, recipe, action)
				return
			}
		}

		deps.Log.Debug("unhandled NPC action",
			zap.String("action", action),
			zap.Int32("npc_id", npc.NpcID),
		)
	}
}

// handleShopBuy — player presses "Buy" — show items NPC sells.
// Sends S_SELL_LIST (opcode 70) = S_ShopSellList in Java (items NPC sells to player).
func handleShopBuy(sess *net.Session, npcID, objID int32, deps *Deps) {
	shop := deps.Shops.Get(npcID)
	if shop == nil || len(shop.SellingItems) == 0 {
		sendNoSell(sess, objID)
		return
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SELL_LIST) // opcode 70
	w.WriteD(objID)
	w.WriteH(uint16(len(shop.SellingItems)))

	for i, si := range shop.SellingItems {
		itemInfo := deps.Items.Get(si.ItemID)
		name := fmt.Sprintf("item#%d", si.ItemID)
		gfxID := int32(0)
		if itemInfo != nil {
			name = itemInfo.Name
			gfxID = itemInfo.InvGfx
		}

		// Append pack count to name if > 1
		if si.PackCount > 1 {
			name = fmt.Sprintf("%s (%d)", name, si.PackCount)
		}

		price := si.SellingPrice

		w.WriteD(int32(i))       // order index
		w.WriteH(uint16(gfxID)) // inventory graphic ID
		w.WriteD(price)          // price
		w.WriteS(name)           // item name

		// Status bytes: show item stats (damage, AC, class restrictions) like Java
		if itemInfo != nil {
			status := buildShopStatusBytes(itemInfo)
			w.WriteC(byte(len(status)))
			w.WriteBytes(status)
		} else {
			w.WriteC(0)
		}
	}

	w.WriteH(0x0007) // currency type: 7 = adena

	sess.Send(w.Bytes())
}

// handleShopSell — player presses "Sell" — show items NPC will buy from player.
// Sends S_SHOP_SELL_LIST (opcode 65) with assessed prices for player's items.
func handleShopSell(sess *net.Session, npcID, objID int32, deps *Deps) {
	shop := deps.Shops.Get(npcID)
	if shop == nil || len(shop.PurchasingItems) == 0 {
		sendNoSell(sess, objID)
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Inv == nil {
		sendNoSell(sess, objID)
		return
	}

	// Build purchasing price lookup
	purchMap := make(map[int32]int32, len(shop.PurchasingItems))
	for _, pi := range shop.PurchasingItems {
		purchMap[pi.ItemID] = pi.PurchasingPrice
	}

	// Find sellable items in player's inventory
	type assessedItem struct {
		objectID int32
		price    int32
	}
	var items []assessedItem
	for _, invItem := range player.Inv.Items {
		price, ok := purchMap[invItem.ItemID]
		if !ok {
			continue
		}
		if invItem.EnchantLvl != 0 || invItem.Bless >= 128 {
			continue // skip enchanted/sealed
		}
		items = append(items, assessedItem{objectID: invItem.ObjectID, price: price})
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
	w.WriteH(0x0007) // currency: adena
	sess.Send(w.Bytes())
}

// handleTeleportURLGeneric shows the NPC's teleport page with data values (prices).
// Handles teleportURL, teleportURLA, teleportURLB, etc.
func handleTeleportURLGeneric(sess *net.Session, npcID, objID int32, action string, deps *Deps) {
	// Look up HTML data (contains htmlID + data values for price display)
	htmlData := deps.TeleportHtml.Get(npcID, action)
	if htmlData != nil {
		sendHypertextWithData(sess, objID, htmlData.HtmlID, htmlData.Data)
		return
	}

	// Fallback: try NpcAction table for teleport_url / teleport_urla
	npcAction := deps.NpcActions.Get(npcID)
	if npcAction == nil {
		return
	}
	lowerAction := strings.ToLower(action)
	switch lowerAction {
	case "teleporturl":
		if npcAction.TeleportURL != "" {
			sendHypertext(sess, objID, npcAction.TeleportURL)
		}
	case "teleporturla":
		if npcAction.TeleportURLA != "" {
			sendHypertext(sess, objID, npcAction.TeleportURLA)
		}
	}
}

// sendHypertext sends S_HYPERTEXT (opcode 39) to show an HTML dialog (no data values).
func sendHypertext(sess *net.Session, objID int32, htmlID string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HYPERTEXT)
	w.WriteD(objID)
	w.WriteS(htmlID)
	w.WriteH(0x00)
	w.WriteH(0)
	sess.Send(w.Bytes())
}

// SendHypertext 開啟 HTML 對話框。Exported for system package usage.
func SendHypertext(sess *net.Session, objID int32, htmlID string) {
	sendHypertext(sess, objID, htmlID)
}

// sendHypertextWithData sends S_HYPERTEXT with data values injected into the HTML template.
// Data values replace %0, %1, %2... placeholders in the client's built-in HTML.
func sendHypertextWithData(sess *net.Session, objID int32, htmlID string, data []string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HYPERTEXT)
	w.WriteD(objID)
	w.WriteS(htmlID)
	if len(data) > 0 {
		w.WriteH(0x01) // has data flag
		w.WriteH(uint16(len(data)))
		for _, val := range data {
			w.WriteS(val)
		}
	} else {
		w.WriteH(0x00)
		w.WriteH(0)
	}
	sess.Send(w.Bytes())
}

// sendNoSell sends S_HYPERTEXT with "nosell" HTML to indicate NPC doesn't trade.
func sendNoSell(sess *net.Session, objID int32) {
	sendHypertext(sess, objID, "nosell")
}

// handleTeleport processes a "teleport xxx" action from the NPC dialog.
// Looks up the destination, checks adena cost, and teleports the player.
func handleTeleport(sess *net.Session, player *world.PlayerInfo, npcID int32, action string, deps *Deps) {
	dest := deps.Teleports.Get(npcID, action)
	if dest == nil {
		deps.Log.Debug("teleport destination not found",
			zap.String("action", action),
			zap.Int32("npc_id", npcID),
		)
		return
	}

	// Check adena cost
	if dest.Price > 0 {
		currentGold := player.Inv.GetAdena()
		if currentGold < dest.Price {
			sendServerMessage(sess, 189) // "金幣不足" (Insufficient adena)
			return
		}

		// Deduct adena
		adenaItem := player.Inv.FindByItemID(world.AdenaItemID)
		if adenaItem != nil {
			adenaItem.Count -= dest.Price
			if adenaItem.Count <= 0 {
				player.Inv.RemoveItem(adenaItem.ObjectID, 0)
				sendRemoveInventoryItem(sess, adenaItem.ObjectID)
			} else {
				sendItemCountUpdate(sess, adenaItem)
			}
		}
	}

	// 出發特效 + 延遲 2 tick（400ms）傳送，讓客戶端播完特效動畫
	SendEffectOnPlayer(sess, player.CharID, 169)
	nearby := deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, sess.ID)
	for _, viewer := range nearby {
		SendEffectOnPlayer(viewer.Session, player.CharID, 169)
	}
	player.ScrollTPTick = 2
	player.ScrollTPX = dest.X
	player.ScrollTPY = dest.Y
	player.ScrollTPMap = dest.MapID

	deps.Log.Info(fmt.Sprintf("玩家傳送  角色=%s  動作=%s  x=%d  y=%d  地圖=%d  花費=%d", player.Name, action, dest.X, dest.Y, dest.MapID, dest.Price))
}

// teleportPlayer moves a player to a new location with full AOI updates.
// Used by NPC teleport, death restart, GM commands, etc.
//
// Packet sequence matches Java Teleportation.actionTeleportation() exactly:
//  1. Remove from old location (broadcast S_REMOVE_OBJECT to old nearby)
//  2. Update world position
//  3. S_MapID — client loads new map
//  4. Broadcast S_OtherCharPacks to new nearby (they see us arrive)
//  5. S_OwnCharPack — self character at new position (live player data)
//  6. updateObject equivalent — send nearby players, NPCs, ground items to self
//  7. S_CharVisualUpdate — weapon/poly visual fix (LAST per Java)
// TeleportPlayer 處理完整傳送流程。Exported for system package usage.
func TeleportPlayer(sess *net.Session, player *world.PlayerInfo, x, y int32, mapID, heading int16, deps *Deps) {
	teleportPlayer(sess, player, x, y, mapID, heading, deps)
}

func teleportPlayer(sess *net.Session, player *world.PlayerInfo, x, y int32, mapID, heading int16, deps *Deps) {
	// 傳送時釋放血盟倉庫鎖定（Java: Teleportation.java 行 122-123）
	if player.ClanID != 0 {
		if clan := deps.World.Clans.GetClan(player.ClanID); clan != nil {
			if clan.WarehouseUsingCharID == player.CharID {
				clan.WarehouseUsingCharID = 0
			}
		}
	}

	// Reset move speed timer (teleport resets speed validation)
	player.LastMoveTime = 0

	// Clear old tile (for NPC pathfinding)
	if deps.MapData != nil {
		deps.MapData.SetImpassable(player.MapID, player.X, player.Y, false)
	}

	// ── 收集玩家擁有的同伴（傳送前）──
	ownedPets := deps.World.GetPetsByOwner(player.CharID)
	ownedSummons := deps.World.GetSummonsByOwner(player.CharID)
	ownedDolls := deps.World.GetDollsByOwner(player.CharID)
	ownedFollower := deps.World.GetFollowerByOwner(player.CharID)

	// 從舊位置附近玩家視野中移除同伴（Java: Teleportation.java removeKnownObject）
	for _, pet := range ownedPets {
		if pet.Dead {
			continue
		}
		oldViewers := deps.World.GetNearbyPlayers(pet.X, pet.Y, pet.MapID, 0)
		removeData := BuildRemoveObject(pet.ID)
		for _, v := range oldViewers {
			if v.CharID != player.CharID {
				v.Session.Send(removeData)
			}
		}
	}
	for _, sum := range ownedSummons {
		if sum.Dead {
			continue
		}
		oldViewers := deps.World.GetNearbyPlayers(sum.X, sum.Y, sum.MapID, 0)
		removeData := BuildRemoveObject(sum.ID)
		for _, v := range oldViewers {
			if v.CharID != player.CharID {
				v.Session.Send(removeData)
			}
		}
	}
	for _, doll := range ownedDolls {
		oldViewers := deps.World.GetNearbyPlayers(doll.X, doll.Y, doll.MapID, 0)
		removeData := BuildRemoveObject(doll.ID)
		for _, v := range oldViewers {
			if v.CharID != player.CharID {
				v.Session.Send(removeData)
			}
		}
	}
	if ownedFollower != nil && !ownedFollower.Dead {
		oldViewers := deps.World.GetNearbyPlayers(ownedFollower.X, ownedFollower.Y, ownedFollower.MapID, 0)
		removeData := BuildRemoveObject(ownedFollower.ID)
		for _, v := range oldViewers {
			if v.CharID != player.CharID {
				v.Session.Send(removeData)
			}
		}
	}

	// 1. 舊位置附近玩家：移除我 + 解鎖我的格子
	oldNearby := deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, sess.ID)
	for _, other := range oldNearby {
		SendRemoveObject(other.Session, player.CharID)
	}

	// 2. 更新世界狀態位置（Java: moveVisibleObject + setLocation）
	deps.World.UpdatePosition(sess.ID, x, y, mapID, heading)

	// 標記新格子不可通行（NPC 尋路用）
	if deps.MapData != nil {
		deps.MapData.SetImpassable(mapID, x, y, true)
	}

	// ── 傳送同伴到新位置（Java: Teleportation.java 寵物跟隨移動）──
	// 方向偏移：將同伴分散在玩家周圍（避免疊在同一格）
	offsets := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	oi := 0
	for _, pet := range ownedPets {
		if pet.Dead {
			continue
		}
		ox, oy := offsets[oi%4][0], offsets[oi%4][1]
		oi++
		deps.World.TeleportPet(pet.ID, x+ox, y+oy, mapID, heading)
	}
	for _, sum := range ownedSummons {
		if sum.Dead {
			continue
		}
		ox, oy := offsets[oi%4][0], offsets[oi%4][1]
		oi++
		deps.World.TeleportSummon(sum.ID, x+ox, y+oy, mapID, heading)
	}
	for _, doll := range ownedDolls {
		ox, oy := offsets[oi%4][0], offsets[oi%4][1]
		oi++
		deps.World.TeleportDoll(doll.ID, x+ox, y+oy, mapID, heading)
	}
	if ownedFollower != nil && !ownedFollower.Dead {
		ox, oy := offsets[oi%4][0], offsets[oi%4][1]
		deps.World.TeleportFollower(ownedFollower.ID, x+ox, y+oy, mapID, heading)
	}

	// 3. S_MapID（即使同地圖也要發——客戶端傳送需要）
	sendMapID(sess, uint16(mapID), false)

	// 重置 Known 集合（傳送 = 完全切換場景）
	if player.Known == nil {
		player.Known = world.NewKnownEntities()
	} else {
		player.Known.Reset()
	}

	// 4. 目的地附近玩家：顯示我 + 封鎖我的格子 + 填入 Known
	newNearby := deps.World.GetNearbyPlayers(x, y, mapID, sess.ID)
	for _, other := range newNearby {
		SendPutObject(other.Session, player)
	}

	// 5. S_OwnCharPack
	sendOwnCharPackPlayer(sess, player)

	// 6. 發送附近實體給自己 + 封鎖格子 + 填入 Known
	for _, other := range newNearby {
		SendPutObject(sess, other)
		player.Known.Players[other.CharID] = world.KnownPos{X: other.X, Y: other.Y}
	}

	nearbyNpcs := deps.World.GetNearbyNpcs(x, y, mapID)
	for _, npc := range nearbyNpcs {
		SendNpcPack(sess, npc)
		player.Known.Npcs[npc.ID] = world.KnownPos{X: npc.X, Y: npc.Y}
	}

	nearbyGnd := deps.World.GetNearbyGroundItems(x, y, mapID)
	for _, g := range nearbyGnd {
		SendDropItem(sess, g)
		player.Known.GroundItems[g.ID] = world.KnownPos{X: g.X, Y: g.Y}
	}

	nearbyDoors := deps.World.GetNearbyDoors(x, y, mapID)
	for _, d := range nearbyDoors {
		SendDoorPerceive(sess, d)
		player.Known.Doors[d.ID] = world.KnownPos{X: d.X, Y: d.Y}
	}

	// 發送同伴 + 附近其他人的同伴（同伴已傳送到新位置，GetNearby* 會包含它們）
	nearbySum := deps.World.GetNearbySummons(x, y, mapID)
	for _, sum := range nearbySum {
		isOwner := sum.OwnerCharID == player.CharID
		masterName := ""
		if m := deps.World.GetByCharID(sum.OwnerCharID); m != nil {
			masterName = m.Name
		}
		SendSummonPack(sess, sum, isOwner, masterName)
		player.Known.Summons[sum.ID] = world.KnownPos{X: sum.X, Y: sum.Y}
		// 也發送給新位置附近的其他玩家（讓他們看到傳送過來的召喚獸）
		if isOwner {
			for _, other := range newNearby {
				SendSummonPack(other.Session, sum, false, player.Name)
			}
		}
	}
	nearbyDolls := deps.World.GetNearbyDolls(x, y, mapID)
	for _, doll := range nearbyDolls {
		masterName := ""
		if m := deps.World.GetByCharID(doll.OwnerCharID); m != nil {
			masterName = m.Name
		}
		SendDollPack(sess, doll, masterName)
		player.Known.Dolls[doll.ID] = world.KnownPos{X: doll.X, Y: doll.Y}
		if doll.OwnerCharID == player.CharID {
			for _, other := range newNearby {
				SendDollPack(other.Session, doll, player.Name)
			}
		}
	}
	nearbyFollowers := deps.World.GetNearbyFollowers(x, y, mapID)
	for _, f := range nearbyFollowers {
		SendFollowerPack(sess, f)
		player.Known.Followers[f.ID] = world.KnownPos{X: f.X, Y: f.Y}
		if f.OwnerCharID == player.CharID {
			for _, other := range newNearby {
				SendFollowerPack(other.Session, f)
			}
		}
	}
	nearbyPets := deps.World.GetNearbyPets(x, y, mapID)
	for _, pet := range nearbyPets {
		isOwner := pet.OwnerCharID == player.CharID
		masterName := ""
		if m := deps.World.GetByCharID(pet.OwnerCharID); m != nil {
			masterName = m.Name
		}
		SendPetPack(sess, pet, isOwner, masterName)
		player.Known.Pets[pet.ID] = world.KnownPos{X: pet.X, Y: pet.Y}
		if isOwner {
			for _, other := range newNearby {
				SendPetPack(other.Session, pet, false, player.Name)
			}
		}
	}

	// 限時地圖偵測（Java: Teleportation.teleportation() 中的 isTimingMap 檢查）
	OnEnterTimedMap(sess, player, mapID)

	// Release client teleport lock (Java: S_Paralysis always sent in finally block).
	sendTeleportUnlock(sess)
}

// handleYesNoResponse processes S_Message_YN dialog responses.
// Routes to trade or party accept/decline based on PendingYesNoType.
func handleYesNoResponse(sess *net.Session, player *world.PlayerInfo, accepted bool, deps *Deps) {
	msgType := player.PendingYesNoType
	data := player.PendingYesNoData
	player.PendingYesNoType = 0
	player.PendingYesNoData = 0

	switch msgType {
	case 252: // Trade confirmation
		handleTradeYesNo(sess, player, data, accepted, deps)
	}
}

// ========================================================================
//  NPC Service Handlers
// ========================================================================

// handleNpcHaste — Haste buffer NPC. Parameters from npc_services.yaml.
func handleNpcHaste(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	h := deps.NpcServices.Haste()
	if npc.NpcID != h.NpcID {
		return
	}
	applyHaste(sess, player, h.DurationSec, h.Gfx, deps)
	sendServerMessage(sess, h.MsgID)
}

// handleNpcActionZero — routes the "0" action based on NPC ID.
// Healer and cancellation NPC parameters from npc_services.yaml.
func handleNpcActionZero(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, objID int32, deps *Deps) {
	// Check if this NPC is a cancellation NPC
	cancel := deps.NpcServices.Cancel()
	if npc.NpcID == cancel.NpcID {
		if player.Level <= cancel.MaxLevel {
			cancelAllBuffs(player, deps)
			broadcastEffect(sess, player, cancel.Gfx, deps)
		}
		return
	}

	// Check if this NPC is a healer
	if healer := deps.NpcServices.GetHealer(npc.NpcID); healer != nil {
		execHeal(sess, player, healer, deps)
		return
	}

	// Unknown "0" action for this NPC — try showing dialog
	npcAction := deps.NpcActions.Get(npc.NpcID)
	if npcAction != nil && npcAction.NormalAction != "" {
		sendHypertext(sess, objID, npcAction.NormalAction)
	}
}

// handleNpcFullHeal — Full heal NPC. Parameters from npc_services.yaml.
func handleNpcFullHeal(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if healer := deps.NpcServices.GetHealer(npc.NpcID); healer != nil {
		execHeal(sess, player, healer, deps)
		return
	}
	// Generic full heal for other healer NPCs not in YAML
	player.HP = player.MaxHP
	player.MP = player.MaxMP
	sendHpUpdate(sess, player)
	sendMpUpdate(sess, player)
	sendServerMessage(sess, 77) // "你覺得舒服多了"
	broadcastEffect(sess, player, 830, deps)
	UpdatePartyMiniHP(player, deps)
}

// execHeal executes a heal service based on YAML-defined healer parameters.
func execHeal(sess *net.Session, player *world.PlayerInfo, h *data.HealerDef, deps *Deps) {
	// Check cost
	if h.Cost > 0 {
		if !consumeAdena(player, h.Cost) {
			sendServerMessageArgs(sess, 337, "$4") // "金幣不足"
			return
		}
		sendAdenaUpdate(sess, player)
	}

	// Apply heal
	switch h.HealType {
	case "random":
		healRange := h.HealMax - h.HealMin + 1
		healAmt := int16(rand.Intn(healRange) + h.HealMin)
		if player.HP < player.MaxHP {
			player.HP += healAmt
			if player.HP > player.MaxHP {
				player.HP = player.MaxHP
			}
		}
		sendHpUpdate(sess, player)
	case "full":
		if h.Target == "hp_mp" || h.Target == "hp" {
			player.HP = player.MaxHP
			sendHpUpdate(sess, player)
		}
		if h.Target == "hp_mp" || h.Target == "mp" {
			player.MP = player.MaxMP
			sendMpUpdate(sess, player)
		}
		UpdatePartyMiniHP(player, deps)
	}

	sendServerMessage(sess, h.MsgID)
	broadcastEffect(sess, player, h.Gfx, deps)
}

// handleNpcWeaponEnchant — Weapon enchanter NPC. Parameters from npc_services.yaml.
func handleNpcWeaponEnchant(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	we := deps.NpcServices.WeaponEnchant()
	weapon := player.Equip.Weapon()
	if weapon == nil {
		sendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	// If already has enchant, cancel old bonus first
	if weapon.DmgByMagic > 0 && weapon.DmgMagicExpiry > 0 {
		weapon.DmgByMagic = 0
		weapon.DmgMagicExpiry = 0
	}

	weapon.DmgByMagic = we.DmgBonus
	weapon.DmgMagicExpiry = we.DurationSec * 5 // seconds → ticks

	recalcEquipStats(sess, player, deps)
	broadcastEffect(sess, player, we.Gfx, deps)
	sendServerMessageArgs(sess, 161, weapon.Name, "$245", "$247")
}

// handleNpcArmorEnchant — Armor enchanter NPC. Parameters from npc_services.yaml.
func handleNpcArmorEnchant(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	ae := deps.NpcServices.ArmorEnchant()
	armor := player.Equip.Get(world.SlotArmor)
	if armor == nil {
		sendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	// If already has enchant, cancel old bonus first
	if armor.AcByMagic > 0 && armor.AcMagicExpiry > 0 {
		armor.AcByMagic = 0
		armor.AcMagicExpiry = 0
	}

	armor.AcByMagic = ae.AcBonus
	armor.AcMagicExpiry = ae.DurationSec * 5 // seconds → ticks

	recalcEquipStats(sess, player, deps)
	broadcastEffect(sess, player, ae.Gfx, deps)
	sendServerMessageArgs(sess, 161, armor.Name, "$245", "$247")
}

// handleNpcPoly — Polymorph NPC. Cost/duration from npc_services.yaml.
func handleNpcPoly(sess *net.Session, player *world.PlayerInfo, polyID int32, deps *Deps) {
	poly := deps.NpcServices.Polymorph()
	if !consumeAdena(player, poly.Cost) {
		sendServerMessageArgs(sess, 337, "$4") // "金幣不足"
		return
	}
	sendAdenaUpdate(sess, player)
	if deps.Polymorph != nil {
		deps.Polymorph.DoPoly(player, polyID, poly.DurationSec, data.PolyCauseNPC)
	}
}

// consumeAdena deducts adena from player inventory. Returns false if insufficient.
func consumeAdena(player *world.PlayerInfo, amount int32) bool {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena == nil || adena.Count < amount {
		return false
	}
	adena.Count -= amount
	return true
}

// sendAdenaUpdate sends the updated adena count to the client after consumption.
func sendAdenaUpdate(sess *net.Session, player *world.PlayerInfo) {
	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena != nil {
		sendItemCountUpdate(sess, adena)
	} else {
		// Adena fully consumed — should have been removed, but just in case
	}
	sendWeightUpdate(sess, player)
}

// ConsumeAdena 匯出 consumeAdena — 供 system 套件扣除金幣。
func ConsumeAdena(player *world.PlayerInfo, amount int32) bool {
	return consumeAdena(player, amount)
}

// SendAdenaUpdate 匯出 sendAdenaUpdate — 供 system 套件更新金幣顯示。
func SendAdenaUpdate(sess *net.Session, player *world.PlayerInfo) {
	sendAdenaUpdate(sess, player)
}

// ========================================================================
//  Crafting System (NPC Item Making)
// ========================================================================


// sendInputAmount sends S_OPCODE_INPUTAMOUNT (136) — S_HowManyMake crafting batch dialog.
// Java: S_HowManyMake(npcObjectId, maxAmount, actionName)
// The client concatenates the two writeS strings with a space separator when sending back C_Amount.
func sendInputAmount(sess *net.Session, npcObjID int32, maxSets int32, action string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_INPUTAMOUNT)
	w.WriteD(npcObjID)
	w.WriteD(0)       // unknown
	w.WriteD(0)       // spinner initial value
	w.WriteD(0)       // spinner minimum
	w.WriteD(maxSets) // spinner maximum
	w.WriteH(0)       // unknown

	// Split action: "request adena2" → prefix="request", suffix="adena2"
	// Client concatenates: "request" + " " + "adena2" = "request adena2" (matches YAML key)
	suffix := action
	if strings.HasPrefix(action, "request ") {
		suffix = action[len("request "):]
	}
	w.WriteS("request")
	w.WriteS(suffix)

	sess.Send(w.Bytes())
}

// SendInputAmount 匯出 sendInputAmount — 供 system/craft.go 發送批量製作對話框。
func SendInputAmount(sess *net.Session, npcObjID int32, maxSets int32, action string) {
	sendInputAmount(sess, npcObjID, maxSets, action)
}


// HandleCraftAmount processes C_Amount (opcode 11) when a crafting batch response is pending.
// Called from HandleHypertextInputResult when player.PendingCraftAction is set.
// Java: C_Amount.java — [D npcObjID][D amount][C unknown][S actionStr]
func HandleCraftAmount(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	action := player.PendingCraftAction
	player.PendingCraftAction = "" // clear pending state

	npcObjID := r.ReadD()
	amount := r.ReadD()
	_ = r.ReadC() // unknown delimiter
	actionStr := r.ReadS()

	if amount <= 0 {
		return
	}

	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		return
	}

	// Distance check
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx > 5 || dy > 5 {
		return
	}

	// Look up recipe — prefer the action string from client, fallback to stored action
	recipe := deps.ItemMaking.Get(actionStr)
	if recipe == nil {
		recipe = deps.ItemMaking.Get(action)
	}
	if recipe == nil {
		return
	}

	if deps.Craft != nil {
		deps.Craft.ExecuteCraft(sess, player, npc, recipe, amount)
	}
}

// ========================================================================
//  火神工匠系統 — Java: L1BlendTable / L1Blend / Npc_CraftDesk
// ========================================================================

// sendCraftItemBlend 使用 ItemBlend 模板顯示指定配方的詳細資訊。
// 3.80C 客戶端的 type 48/49 拖放介面無法使用，且不支援 inline HTML（htmlID 用於讀取本地對話檔）。
// 改用客戶端已有的 ItemBlend 模板（3.80C 確認可用）呈現配方。
// 玩家點 "confirm craft" → 開啟交易視窗確認；"cancel craft" → 顯示下一個配方。
//
// Java ItemBlend 模板資料格式：
//
//	data[0] = 成品名稱
//	data[1] = 額外獎勵資訊（空字串 = 無）
//	data[2] = 等級限制（" 無限制 " 或 " XX級以上。 "）
//	data[3] = 職業限制（" 所有職業" 或具體職業名）
//	data[4] = 成功機率（" XX %" 或空字串 = 100%）
//	data[5] = 增加機率道具資訊（空字串）
//	data[6] = 替代材料資訊（空字串）
//	data[7+] = 材料條目（"材料名 (數量) 個"）
func sendCraftItemBlend(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps, index int) {
	if deps.ItemMaking == nil {
		sendGlobalChat(sess, 9, "\\f3製作系統尚未啟用。")
		return
	}
	recipes := deps.ItemMaking.GetByNpcID(npc.NpcID)
	if len(recipes) == 0 {
		sendGlobalChat(sess, 9, "\\f3此 NPC 沒有可用的配方。")
		return
	}

	// 循環索引
	if index < 0 || index >= len(recipes) {
		index = 0
	}
	recipe := recipes[index]

	// 儲存瀏覽狀態
	player.PendingCraftKey = recipe.Action
	player.PendingCraftNpcID = npc.NpcID
	player.PendingCraftIndex = index

	// 組裝 data[0]: 成品名稱
	productName := recipe.Note
	if len(recipe.Items) > 0 {
		out := recipe.Items[0]
		if info := deps.Items.Get(out.ItemID); info != nil {
			productName = info.Name
			if out.EnchantLvl > 0 {
				productName = fmt.Sprintf("+%d %s", out.EnchantLvl, productName)
			}
			if out.Amount > 1 {
				productName = fmt.Sprintf("%s (%d)", productName, out.Amount)
			}
		}
	}

	// data[1]: 額外獎勵
	bonusInfo := ""
	if recipe.BonusItemID > 0 {
		if info := deps.Items.Get(recipe.BonusItemID); info != nil {
			bonusInfo = fmt.Sprintf("製造成功時額外獲得: %s", info.Name)
			if recipe.BonusItemCount > 1 {
				bonusInfo += fmt.Sprintf(" (%d)", recipe.BonusItemCount)
			}
		}
	}

	// data[2]: 等級限制
	levelInfo := " 無限制 "
	if recipe.RequiredLevel > 0 {
		levelInfo = fmt.Sprintf(" %d級以上。 ", recipe.RequiredLevel)
	}

	// data[3]: 職業限制
	classInfo := " 所有職業"
	if recipe.RequiredClass > 0 {
		if name := classIDToName(recipe.RequiredClass); name != "" {
			classInfo = " " + name
		}
	}

	// data[4]: 成功機率（未設定或 0 視為 100%）
	rate := recipe.SuccessRate
	if rate <= 0 {
		rate = 100
	}
	rateInfo := fmt.Sprintf(" %d %%", rate)

	// data[5], data[6]: 空字串（增加機率道具、替代材料）
	// data[7+]: 材料條目
	matCount := len(recipe.Materials)
	msgs := make([]string, 7+matCount)
	msgs[0] = productName
	msgs[1] = bonusInfo
	msgs[2] = levelInfo
	msgs[3] = classInfo
	msgs[4] = rateInfo
	msgs[5] = ""
	msgs[6] = fmt.Sprintf("(%d/%d)", index+1, len(recipes))

	for i, mat := range recipe.Materials {
		matName := fmt.Sprintf("item#%d", mat.ItemID)
		if info := deps.Items.Get(mat.ItemID); info != nil {
			matName = info.Name
		}
		if mat.EnchantLvl > 0 {
			matName = fmt.Sprintf("+%d %s", mat.EnchantLvl, matName)
		}
		msgs[7+i] = fmt.Sprintf("%s (%d) 個", matName, mat.Amount)
	}

	sendHypertextWithData(sess, npc.ID, "ItemBlend", msgs)
}

// handleRequestCraft 處理 "request craft" — 顯示配方清單。
// 815 版：smithitem 有 "request craft" 按鈕 → 送 smithitem1（41 個配方名稱）。
// 3.80C：smithitem 直接顯示配方清單（desc-c.tbl），不一定有 "request craft" 按鈕。
// 保留此函式作為相容性備用。
func handleRequestCraft(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if deps.ItemMaking == nil {
		return
	}
	recipes := deps.ItemMaking.GetByNpcID(npc.NpcID)
	if len(recipes) == 0 {
		return
	}

	const smithitem1Slots = 41 // Java: msg0~msg40，固定 41 格
	msgs := make([]string, smithitem1Slots)
	for i := 0; i < smithitem1Slots && i < len(recipes); i++ {
		msgs[i] = recipes[i].Note
	}

	sendHypertextWithData(sess, npc.ID, "smithitem1", msgs)
}

// handleCraftSelect 處理配方選擇 — 開啟交易視窗顯示成品與材料。
// 交易視窗佈局：
//   - 上方（panelType=0, 玩家側）：成品預覽
//   - 下方（panelType=1, 對方側）：需要的材料
//
// 3.80C 客戶端在同一 tick 收到 S_Trade + S_TradeAddItem 時，交易視窗尚未初始化完成，
// 導致物品不顯示。因此 S_Trade 立即發送，S_TradeAddItem 延遲 1 tick 由 CraftTradeSystem 發送。
// 玩家按確認（C_ACCEPT_XCHG）→ handleCraftTradeConfirm 執行製作。
func handleCraftSelect(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, recipe *data.CraftRecipe, deps *Deps) {
	// 儲存選中的配方，交易確認時使用
	player.PendingCraftKey = recipe.Action
	player.PendingCraftNpcID = npc.NpcID

	// 開啟交易視窗 — 對方名稱（下方標題）顯示「需要的材料」
	sendTradeOpen(sess, "需要的材料")

	// 延遲 1 tick 發送物品（等待客戶端初始化交易視窗）
	player.CraftTradeTick = 1
}

// SendCraftTradeItems 延遲發送製作交易視窗的物品（由 CraftTradeSystem 呼叫）。
// 根據 PendingCraftKey/NpcID 查找配方，發送成品與材料。
// 物品不存在於 YAML 時使用 fallback 值（名稱顯示 "item#ID"，GfxID 使用 InvGfx 或預設 24）。
func SendCraftTradeItems(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.PendingCraftKey == "" || deps.ItemMaking == nil {
		return
	}

	recipe := deps.ItemMaking.GetByNpcAction(player.PendingCraftNpcID, player.PendingCraftKey)
	if recipe == nil {
		return
	}

	// 上方（panelType=0, 玩家側）：成品預覽
	// Java S_TradeAddItem 使用 item.getItem().getGfxId() = 地面圖示（GrdGfx）
	for _, out := range recipe.Items {
		gfx, viewName, bless := craftTradeItemInfo(out.ItemID, out.Amount, out.EnchantLvl, deps)
		sendTradeAddItem(sess, gfx, viewName, bless, 0)
	}

	// 下方（panelType=1, 對方側）：需要的材料
	for _, mat := range recipe.Materials {
		gfx, viewName, bless := craftTradeItemInfo(mat.ItemID, mat.Amount, mat.EnchantLvl, deps)
		sendTradeAddItem(sess, gfx, viewName, bless, 1)
	}
}

// craftTradeItemInfo 取得物品的交易視窗顯示資訊。
// 物品存在於 YAML → 使用真實 GrdGfx、名稱、bless。
// 物品不存在 → 使用 fallback GfxID 24（常見物品圖示）、"item#ID" 名稱、bless=0。
func craftTradeItemInfo(itemID, amount, enchantLvl int32, deps *Deps) (gfx uint16, viewName string, bless byte) {
	info := deps.Items.Get(itemID)
	if info != nil {
		gfx = uint16(info.InvGfx)
		viewName = info.Name
		bless = byte(info.Bless)
	} else {
		// 物品未定義時的 fallback：GfxID 24（寶石圖示），名稱用 item#ID
		gfx = 24
		viewName = fmt.Sprintf("item#%d", itemID)
		bless = 0
	}
	if enchantLvl > 0 {
		viewName = fmt.Sprintf("+%d %s", enchantLvl, viewName)
	}
	if amount > 1 {
		viewName = fmt.Sprintf("%s (%d)", viewName, amount)
	}
	return
}

// craftItemDisplayName 組裝成品顯示名稱（模擬 Java getLogName()）
func craftItemDisplayName(itemID, amount, enchantLvl int32, deps *Deps) string {
	name := fmt.Sprintf("item#%d", itemID)
	if info := deps.Items.Get(itemID); info != nil {
		name = info.Name
	}
	if enchantLvl > 0 {
		name = fmt.Sprintf("+%d %s", enchantLvl, name)
	}
	if amount > 1 {
		name += fmt.Sprintf(" (%d)", amount)
	}
	return name
}

// handleConfirmCraft 處理 "confirm craft" — 開啟交易視窗預覽成品與材料。
// 直接送 S_Trade 開啟視窗，延遲 1 tick 發 S_TradeAddItem。
func handleConfirmCraft(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if player.PendingCraftKey == "" || deps.ItemMaking == nil || deps.Craft == nil {
		return
	}

	// 驗證 NPC 一致性
	if player.PendingCraftNpcID != npc.NpcID {
		player.PendingCraftKey = ""
		player.PendingCraftNpcID = 0
		player.PendingCraftIndex = 0
		player.CraftTradeTick = 0
		return
	}

	recipe := deps.ItemMaking.GetByNpcAction(player.PendingCraftNpcID, player.PendingCraftKey)
	if recipe == nil {
		player.PendingCraftKey = ""
		player.PendingCraftNpcID = 0
		player.PendingCraftIndex = 0
		player.CraftTradeTick = 0
		return
	}

	// 開啟交易視窗 + 延遲 1 tick 發送物品
	handleCraftSelect(sess, player, npc, recipe, deps)
}

// classIDToName 將職業 ID 轉換為顯示名稱。
func classIDToName(classID int32) string {
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

// sendRefineUI 發送火神精煉/合成 UI 封包。
// sendRefineUI 發送火神精煉/合成 UI 封包。
// Java S_Refine.java（380火神煉化）：opcode 64 + type + npcObjID + 尾碼
// Java S_EquipmentWindow（815版）：同格式但尾碼不同
// 客戶端回應走 C_PledgeContent (opcode 78) type=13（精煉）或 type=14（合成）。
// type=48: 精煉（itemresolve），type=49: 合成（itemtransform）
//
// 尾碼測試記錄：
// - 0x95, 0x19（S_EquipmentWindow 815）：視窗開啟，無法拖裝備
// - 0xE3, 0x92（S_Refine 380）：視窗開啟，無法拖裝備
func sendRefineUI(sess *net.Session, npcObjID int32, refineType byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHARSYNACK)
	w.WriteC(refineType) // 48=精煉, 49=合成
	w.WriteD(npcObjID)   // NPC object ID
	w.WriteC(0)          // 尾碼（3.80C 正確值待確認）
	w.WriteC(0)
	sess.Send(w.Bytes())
}

// sendFireSmithSellList 發送火神精煉介面（分解物品換結晶）。
// Java: S_ShopBuyListFireSmith — 使用 S_OPCODE_SHOP_SELL_LIST（opcode 65）。
// 格式與商店賣出列表完全相同，「價格」欄位填入結晶數量。
// 客戶端回傳 C_Result type=1（賣出），伺服器攔截後給予結晶而非金幣。
func sendFireSmithSellList(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if deps.FireCrystals == nil {
		sendGlobalChat(sess, 9, "\\f3火神精煉系統尚未啟用。")
		return
	}

	// 排除的物品 ID（Java: S_ShopBuyListFireSmith.assessItems）
	excludeItems := map[int32]bool{
		40308: true, // 金幣
		41246: true, // 魔法結晶體
		44070: true, // 天寶
		40314: true, // 項圈
		40316: true, // 高等寵物項圈
		83000: true, // 貝利
		83022: true, // 黃金貝利
		80033: true, // 推廣銀幣
	}

	type assessedItem struct {
		objectID     int32
		crystalCount int32
	}
	var items []assessedItem

	for _, invItem := range player.Inv.Items {
		// 跳過排除物品
		if excludeItems[invItem.ItemID] {
			continue
		}
		// 跳過已裝備物品
		if invItem.Equipped {
			continue
		}

		itemInfo := deps.Items.Get(invItem.ItemID)
		if itemInfo == nil {
			continue
		}
		// 只處理武器和防具（Java: type2 != 0）
		if itemInfo.Category == data.CategoryEtcItem {
			continue
		}

		// 計算基礎 item ID（去除祝福/詛咒偏移）
		// Java: bless==0 → itemId-100000; bless==2 → itemId-200000
		lookupID := invItem.ItemID
		if invItem.Bless == 0 { // 祝福狀態
			candidateID := invItem.ItemID - 100000
			if candidateInfo := deps.Items.Get(candidateID); candidateInfo != nil {
				if candidateInfo.Name == itemInfo.Name {
					lookupID = candidateID
				}
			}
		} else if invItem.Bless == 2 { // 詛咒狀態
			candidateID := invItem.ItemID - 200000
			if candidateInfo := deps.Items.Get(candidateID); candidateInfo != nil {
				if candidateInfo.Name == itemInfo.Name {
					lookupID = candidateID
				}
			}
		}

		entry := deps.FireCrystals.Get(lookupID)
		if entry == nil {
			continue
		}

		crystalCount := entry.GetCrystalCount(int(invItem.EnchantLvl), int(itemInfo.Category), itemInfo.SafeEnchant)
		if crystalCount > 0 {
			items = append(items, assessedItem{objectID: invItem.ObjectID, crystalCount: crystalCount})
		}
	}

	if len(items) == 0 {
		// 無可精煉物品（Java: S_NPCTalkReturn "smithitem3"）
		sendHypertext(sess, npc.ID, "smithitem3")
		return
	}

	// 標記玩家正在使用火神精煉（用於 C_Result 攔截）
	player.FireSmithNpcObjID = npc.ID

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SHOP_SELL_LIST)
	w.WriteD(npc.ID)
	w.WriteH(uint16(len(items)))
	for _, it := range items {
		w.WriteD(it.objectID)     // 物品 object ID
		w.WriteD(it.crystalCount) // 結晶數量（顯示為「價格」）
	}
	w.WriteH(0x0007) // 幣種: 7=金幣（客戶端顯示用）
	sess.Send(w.Bytes())
}

// SendCloseList 關閉 NPC 對話視窗。
// Java: S_CloseList → opcode 39 + writeD(objID) + writeS("")
func SendCloseList(sess *net.Session, objID int32) {
	sendHypertext(sess, objID, "")
}

// ========================================================================
//  Summon control — Java: L1ActionSummon.action()
// ========================================================================

// handleSummonAction processes summon control commands from the moncom dialog.
// Action strings: "aggressive", "defensive", "stay", "extend", "alert", "dismiss".
func handleSummonAction(sess *net.Session, player *world.PlayerInfo, sum *world.SummonInfo, action string, deps *Deps) {
	switch action {
	case "aggressive":
		sum.Status = world.SummonAggressive
	case "defensive":
		sum.Status = world.SummonDefensive
		sum.AggroTarget = 0
		sum.AggroPlayerID = 0
	case "stay":
		sum.Status = world.SummonRest
		sum.AggroTarget = 0
		sum.AggroPlayerID = 0
	case "extend":
		sum.Status = world.SummonExtend
		sum.AggroTarget = 0
		sum.AggroPlayerID = 0
	case "alert":
		sum.Status = world.SummonAlert
		sum.HomeX = sum.X
		sum.HomeY = sum.Y
		sum.AggroTarget = 0
		sum.AggroPlayerID = 0
	case "dismiss":
		DismissSummon(sum, player, deps)
		return
	}
	// Refresh menu with updated status
	sendSummonMenu(sess, sum)
}

// isNumericString returns true if s is a non-empty string of ASCII digits.
// Java: cmd.matches("[0-9]+") — used to detect summon selection responses.
func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ---------- 欄位開放專家 史奈普（NPC 81445）----------
// Java: C_NPCAction.java — npcId == 81445
// 動作 A = Lv76 戒指欄位（任務 79）
// 動作 B = Lv81 戒指欄位（任務 80）
// 動作 C = Lv85 護符欄位（任務 82，自訂功能）
func handleSlotNpc(sess *net.Session, player *world.PlayerInfo, npcObjID int32, action string, deps *Deps) {
	switch action {
	case "a": // Lv76 戒指欄（第3個戒指欄位）
		if player.IsQuestDone(79) {
			SendServerMessage(sess, 3254) // 已經開通
			return
		}
		player.PendingYesNoType = 3312
		player.PendingYesNoData = 76
		sendYesNoDialog(sess, 3312)

	case "b": // Lv81 戒指欄（第4個戒指欄位）
		if player.IsQuestDone(80) {
			SendServerMessage(sess, 3254) // 已經開通
			return
		}
		player.PendingYesNoType = 3313
		player.PendingYesNoData = 81
		sendYesNoDialog(sess, 3313)

	case "c": // Lv85 護符欄（自訂擴充欄位）
		if player.IsQuestDone(82) {
			SendServerMessage(sess, 3254) // 已經開通
			return
		}
		player.PendingYesNoType = 3590
		player.PendingYesNoData = 85
		sendYesNoDialog(sess, 3590)
	}
}
