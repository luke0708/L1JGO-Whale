package handler

import (
	"context"
	"time"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// HandleAttr processes C_ATTR (opcode 121) — 多用途封包。
// Java C_Attr 格式：
//   mode = readH()
//   if mode == 0 { readD(); mode = readH() }  ← 前綴跳過
//   switch(mode) { case 479: 加點; case 97/252/630/951/953/954: yes/no 回應 }
func HandleAttr(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	mode := r.ReadH()

	// Java: mode == 0 時，先讀 D（target objID）再讀一次 H 取得真正 mode
	if mode == 0 {
		_ = r.ReadD() // tgobjid（RaiseAttr 帶的 charID）
		mode = r.ReadH()
	}

	if mode == statAllocAttrCode { // 479 = 加點（Java C_Attr case 479）
		confirm := r.ReadC()
		handleStatAlloc(sess, mode, confirm, r, deps)
		return
	}

	// Java: switch(mode) 後每個 case 都讀 readC() (1 byte)。
	// 之前錯誤地多讀了 ReadD()+ReadH()+ReadH() 共 8 bytes，
	// 客戶端封包中不存在這些欄位，Reader 返回 0 → 所有回應被靜默丟棄。
	response := r.ReadC() // 0=No, 1=Yes（Java: readC()）
	accepted := response != 0

	deps.Log.Debug("C_Attr yes/no 回應",
		zap.Uint16("mode", mode),
		zap.Uint8("response", response),
		zap.Bool("accepted", accepted),
	)

	// Clear pending state
	player.PendingYesNoType = 0
	data := player.PendingYesNoData
	player.PendingYesNoData = 0

	switch mode {
	case 252: // Trade confirmation
		handleTradeYesNo(sess, player, data, accepted, deps)

	case 951: // Chat party invite: 您要接受玩家 %0 提出的隊伍對話邀請嗎？(Y/N)
		HandleChatPartyInviteResponse(player, data, accepted, deps)

	case 953: // Normal party invite: 玩家 %0 邀請您加入隊伍？(Y/N)
		HandlePartyInviteResponse(player, data, accepted, deps)

	case 954: // Auto-share party invite: 玩家 %0 邀請您加入自動分配隊伍？(Y/N)
		HandlePartyInviteResponse(player, data, accepted, deps)

	case 97: // Clan join request: %0想加入你的血盟，是否同意？(Y/N)
		HandleClanJoinResponse(sess, player, data, accepted, deps)

	case 630: // 決鬥確認: %0 要與你決鬥。你是否同意？(Y/N)
		HandleDuelResponse(sess, player, data, accepted, deps)

	case 3312: // Lv76 戒指欄位開通（NPC 81445 史奈普）
		handleSlotUnlock(sess, player, data, accepted, 76, 79, 10_000_000, deps)

	case 3313: // Lv81 戒指欄位開通（NPC 81445 史奈普）
		handleSlotUnlock(sess, player, data, accepted, 81, 80, 30_000_000, deps)

	case 3590: // Lv85 護符欄位開通（NPC 81445 史奈普，自訂功能）
		handleSlotUnlock(sess, player, data, accepted, 85, 82, 20_000_000, deps)

	case 654: // 求婚回應（Java: C_Attr case 654）
		HandleMarriageAccept(sess, player, data, accepted, deps)

	case 653: // 離婚確認（Java: C_Attr case 653）
		HandleDivorceConfirm(sess, player, accepted, deps)

	case 512: // 血盟小屋改名（Java: C_Attr case 512）
		// Java: readC() 已在上方讀取，接著 readS() 取得新名稱
		houseName := r.ReadS()
		if accepted && data != 0 {
			HandleHouseRename(sess, player, data, houseName, deps)
		}

	case 223: // 聯盟邀請回應（Java: C_Attr case 223）— 暫存 stub
		// TODO: 處理聯盟邀請接受/拒絕
	}
}

// handleSlotUnlock 處理戒指欄位開通確認回應。
// Java: C_Attr.java case 3312/3313 — 檢查等級、金幣、任務前置 → 扣金幣 → 完成任務 → 特效。
func handleSlotUnlock(sess *net.Session, player *world.PlayerInfo, pendingData int32,
	accepted bool, reqLevel int16, questID int32, goldCost int32, deps *Deps) {

	if !accepted {
		return
	}

	// 防重放：PendingYesNoData 必須與要求的等級一致
	if pendingData != int32(reqLevel) {
		return
	}

	// 已經完成過
	if player.IsQuestDone(questID) {
		return
	}

	// Lv81 前置條件：必須先完成 Lv76 戒指欄位
	if questID == 80 && !player.IsQuestDone(79) {
		SendServerMessage(sess, 3253) // 條件不足
		return
	}

	// 等級不足
	if player.Level < reqLevel {
		SendServerMessage(sess, 3253)
		sendHypertext(sess, player.CharID, "slot3")
		return
	}

	// 扣金幣（委派給 NpcServiceSystem）
	if deps.NpcSvc == nil || !deps.NpcSvc.ConsumeAdena(sess, player, goldCost) {
		SendServerMessage(sess, 3253)
		sendHypertext(sess, player.CharID, "slot3")
		return
	}

	// --- 任務完成：寫 DB + 更新記憶體 ---
	if deps.QuestRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := deps.QuestRepo.SetCompleted(ctx, player.CharID, questID)
		cancel()
		if err != nil {
			deps.Log.Error("戒指欄位任務寫入失敗",
				zap.Int32("charID", player.CharID),
				zap.Int32("questID", questID),
				zap.Error(err),
			)
		}
	}
	player.SetQuestStep(questID, 255)

	// --- 欄位圖示解鎖 + 特效 + 成功對話 ---
	sendSlotExpansion(sess, questID)
	SendEffectOnPlayer(sess, player.CharID, 12003)
	sendHypertext(sess, player.CharID, "slot9")

	deps.Log.Info("戒指欄位開通",
		zap.String("name", player.Name),
		zap.Int32("questID", questID),
		zap.Int16("reqLevel", reqLevel),
	)
}

// sendSlotExpansion 發送 S_CharReset(67, 1, value) — 通知客戶端解鎖裝備欄位圖示。
// Java: S_CharReset.java 擴充欄位構造函式（type=67, subType=1=戒指/耳環）。
// value 映射：79→7(Ring3/Lv76), 80→15(Ring4/Lv81)。
func sendSlotExpansion(sess *net.Session, questID int32) {
	var value byte
	switch questID {
	case 79:
		value = 7 // Ring3（Lv76 戒指欄）
	case 80:
		value = 15 // Ring4（Lv81 戒指欄）
	default:
		return
	}
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHARSYNACK)
	w.WriteC(67) // sub-type: 擴充欄位
	w.WriteD(1)  // subType: 1=耳環/戒指
	w.WriteC(value)
	for i := 0; i < 6; i++ {
		w.WriteD(0)
	}
	w.WriteH(0)
	sess.Send(w.Bytes())
}
