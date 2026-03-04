package handler

import (
	"fmt"
	"math"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// handlePagedTeleportTalk shows the category menu (y_t_0) when the player
// clicks a paginated teleporter NPC (e.g., NPC 91053).
// Java: Npc_Teleport.action() → first interaction sends category list.
func handlePagedTeleportTalk(sess *net.Session, player *world.PlayerInfo, objID int32, deps *Deps) {
	cats := deps.TeleportPages.Categories()
	if len(cats) == 0 {
		return
	}
	// Store NPC objID for subsequent actions
	player.TeleNpcObjID = objID
	player.TeleCategory = ""
	player.TelePage = 0
	sendHypertextWithData(sess, objID, "y_t_0", cats)
}

// handlePagedTeleportAction processes actions within the paginated teleport dialog.
// Action strings: category names (e.g., "A", "B"), "up", "dn", digits "0"-"9".
func handlePagedTeleportAction(sess *net.Session, player *world.PlayerInfo, npc *world.NpcInfo, action string, deps *Deps) {
	switch action {
	case "up":
		// Previous page
		if player.TelePage > 0 {
			player.TelePage--
		}
		showTelePage(sess, player, npc.ID, deps)

	case "dn":
		// Next page
		dests := deps.TeleportPages.GetCategory(player.TeleCategory)
		totalPages := (len(dests) + 9) / 10
		if player.TelePage < totalPages-1 {
			player.TelePage++
		}
		showTelePage(sess, player, npc.ID, deps)

	case "del":
		// Java: clears time map (cooldown) — no-op for us

	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Select destination by digit
		digit := int(action[0] - '0')
		idx := player.TelePage*10 + digit
		dests := deps.TeleportPages.GetCategory(player.TeleCategory)
		if idx < 0 || idx >= len(dests) {
			return
		}
		dest := &dests[idx]
		executeTeleportPage(sess, player, dest, deps)

	default:
		// Category selection — action is the category name (e.g., "A", "B", "H01")
		dests := deps.TeleportPages.GetCategory(action)
		if dests == nil {
			return
		}
		player.TeleCategory = action
		player.TelePage = 0
		showTelePage(sess, player, npc.ID, deps)
	}
}

// showTelePage sends the current page of destinations to the client.
// Uses y_t_1 (single page), y_t_2 (middle), y_t_3 (first), y_t_4 (last).
func showTelePage(sess *net.Session, player *world.PlayerInfo, npcObjID int32, deps *Deps) {
	dests := deps.TeleportPages.GetCategory(player.TeleCategory)
	if len(dests) == 0 {
		return
	}

	page := player.TelePage
	totalPages := (len(dests) + 9) / 10
	start := page * 10
	end := start + 10
	if end > len(dests) {
		end = len(dests)
	}

	// Build data strings: "{name}隊伍:{partySize} ({itemName}-{price}),"
	partySize := 1
	if player.PartyID != 0 {
		party := deps.World.Parties.GetParty(player.CharID)
		if party != nil {
			partySize = len(party.Members)
		}
	}

	dataStrings := make([]string, end-start)
	for i := start; i < end; i++ {
		d := &dests[i]
		itemName := resolveItemName(d.ItemID, deps)
		dataStrings[i-start] = fmt.Sprintf("%s隊伍:%d (%s-%d),", d.Name, partySize, itemName, d.Price)
	}

	// Determine HTML template based on pagination state
	var htmlID string
	if totalPages == 1 {
		htmlID = "y_t_1" // single page, no navigation
	} else if page == 0 {
		htmlID = "y_t_3" // first page, next only
	} else if page == totalPages-1 {
		htmlID = "y_t_4" // last page, prev only
	} else {
		htmlID = "y_t_2" // middle page, both prev/next
	}

	sendHypertextWithData(sess, npcObjID, htmlID, dataStrings)
}

// executeTeleportPage performs the actual teleport for a paginated destination.
// Checks level restriction, item cost (adena), and teleports the player.
func executeTeleportPage(sess *net.Session, player *world.PlayerInfo, dest *data.TeleportPageDest, deps *Deps) {
	// Level check
	if dest.MaxLevel > 0 && player.Level > dest.MaxLevel {
		sendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	// Item cost check (item_id 40308 = adena)
	if dest.Price > 0 {
		currentGold := player.Inv.GetAdena()
		if currentGold < dest.Price {
			sendServerMessage(sess, 189) // "金幣不足"
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

	// Clear teleport session state
	player.TeleCategory = ""
	player.TelePage = 0
	player.TeleNpcObjID = 0

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

	deps.Log.Info(fmt.Sprintf("分頁傳送  角色=%s  目的地=%s  x=%d  y=%d  地圖=%d  花費=%d",
		player.Name, dest.Name, dest.X, dest.Y, dest.MapID, dest.Price))
}

// resolveItemName returns the display name for an item ID, used in teleport cost display.
func resolveItemName(itemID int32, deps *Deps) string {
	info := deps.Items.Get(itemID)
	if info != nil {
		return info.Name
	}
	return fmt.Sprintf("item#%d", itemID)
}

// isPagedTeleportAction returns true if the action string is a valid paginated
// teleport command (category, page nav, or digit selection).
func isPagedTeleportAction(action string, deps *Deps) bool {
	switch action {
	case "up", "dn", "del":
		return true
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return true
	default:
		// Check if it's a category name
		return deps.TeleportPages.GetCategory(action) != nil
	}
}

// isNearNpc checks if the player is within interaction range (5 tiles) of the NPC.
func isNearNpc(player *world.PlayerInfo, npc *world.NpcInfo) bool {
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	return dx <= 5 && dy <= 5
}
