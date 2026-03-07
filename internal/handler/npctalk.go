package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"go.uber.org/zap"
)

// HandleNpcTalk processes C_DIALOG (opcode 34) — player clicks an NPC.
// Looks up the NPC's dialog HTML ID and sends S_HYPERTEXT (opcode 39).
func HandleNpcTalk(sess *net.Session, r *packet.Reader, deps *Deps) {
	objID := r.ReadD()

	// Check if target is a summon — show summon control menu
	if sum := deps.World.GetSummon(objID); sum != nil {
		player := deps.World.GetBySession(sess.ID)
		if player != nil && sum.OwnerCharID == player.CharID {
			sendSummonMenu(sess, sum)
		}
		return
	}

	// Check if target is a pet — show pet control menu ("anicom")
	if pet := deps.World.GetPet(objID); pet != nil {
		player := deps.World.GetBySession(sess.ID)
		if player != nil && pet.OwnerCharID == player.CharID && deps.PetLife != nil {
			sendPetMenu(sess, pet, deps.PetLife.PetExpPercent(pet))
		}
		return
	}

	npc := deps.World.GetNpc(objID)
	if npc == nil {
		return
	}

	// Check if this is a paginated teleporter NPC (e.g., NPC 91053)
	if deps.TeleportPages != nil && deps.TeleportPages.IsPageTeleportNpc(npc.NpcID) {
		player := deps.World.GetBySession(sess.ID)
		if player != nil {
			handlePagedTeleportTalk(sess, player, objID, deps)
		}
		return
	}

	// 排名 NPC（80026-80029）— 需要帶資料的 S_NPCTalkReturn
	if isRankingNpc(npc.NpcID) {
		player := deps.World.GetBySession(sess.ID)
		if player != nil {
			handleRankingNpcTalk(sess, player, objID, npc, deps)
		}
		return
	}

	// L1AuctionBoard（拍賣佈告欄 NPC）— 依 NPC 座標過濾該城鎮的拍賣列表
	if npc.Impl == "L1AuctionBoard" {
		handleAuctionTalk(sess, objID, npc.X, npc.Y, deps)
		return
	}

	// L1Housekeeper（管家 NPC）— Java L1HousekeeperInstance.onTalkAction()
	if npc.Impl == "L1Housekeeper" {
		player := deps.World.GetBySession(sess.ID)
		if player != nil {
			handleHousekeeperTalk(sess, player, objID, npc.NpcID, deps)
		}
		return
	}

	// L1Dwarf（倉庫 NPC）— Java L1DwarfInstance.onTalkAction() 對所有倉庫 NPC
	// 強制回傳 "storage"（3.53C 新版倉庫介面），客戶端內建索回＋存放兩個 tab。
	// 只有 NPC 60028（精靈倉庫）對非精靈玩家回傳 "elCE1" 拒絕訊息。
	if npc.Impl == "L1Dwarf" {
		player := deps.World.GetBySession(sess.ID)
		if player == nil {
			return
		}
		htmlID := "storage"
		if npc.NpcID == 60028 && player.ClassType != 2 { // 2=精靈
			htmlID = "elCE1"
		}
		sendHypertext(sess, objID, htmlID)
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	// 任務 NPC 對話 — 依玩家任務進度顯示不同 htmlid
	if handleQuestNpcTalk(sess, player, objID, npc.NpcID, deps) {
		return
	}

	// Look up dialog data for this NPC template
	action := deps.NpcActions.Get(npc.NpcID)
	if action == nil {
		deps.Log.Debug("NPC has no dialog action",
			zap.Int32("npc_id", npc.NpcID),
			zap.String("name", npc.Name),
		)
		return
	}

	htmlID := action.NormalAction
	if player.Lawful < -1000 && action.CaoticAction != "" {
		htmlID = action.CaoticAction
	}
	if htmlID == "" {
		return
	}

	// Send S_HYPERTEXT (opcode 39) — NPC dialog
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HYPERTEXT)
	w.WriteD(objID)    // NPC object ID
	w.WriteS(htmlID)   // HTML identifier (client looks up built-in HTML)
	w.WriteH(0x00)     // no arguments marker
	w.WriteH(0)        // argument count = 0
	sess.Send(w.Bytes())
}
