package handler

import (
	"fmt"
	"strconv"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// --- 排名 NPC 動作處理器 ---
// NPC 80026 = 殺手風雲榜, 80027 = 財富風雲榜
// NPC 80028 = 血盟風雲榜, 80029 = 英雄風雲榜
// Java: Npc_KillerRanking, Npc_WealthRanking, Npc_ClanRanking, Npc_HeroRanking

// isRankingNpc 判斷 NPC 是否為排名 NPC。
func isRankingNpc(npcID int32) bool {
	return npcID >= 80026 && npcID <= 80029
}

// handleRankingNpcTalk 處理排名 NPC 初始對話（C_DIALOG）。
// Java: Npc_*Ranking.talk() → S_NPCTalkReturn(npcObjID, htmlID, data[])
func handleRankingNpcTalk(sess *net.Session, player *world.PlayerInfo, objID int32, npc *world.NpcInfo, deps *Deps) {
	if deps.Ranking == nil {
		return
	}

	switch npc.NpcID {
	case 80029: // 英雄風雲榜 — 顯示職業選擇（無資料）
		sendHypertext(sess, objID, "y_h_1")

	case 80028: // 血盟風雲榜 — 顯示血盟 TOP10 + 宣戰選項
		entries := deps.Ranking.GetClanRanking()
		data := make([]string, 20) // 前 10 = 血盟名(人數)，後 10 = 宣戰選項
		for i := 0; i < 10; i++ {
			if i < len(entries) {
				// Java 格式："血盟名,人數" → "血盟名(人數)"
				data[i] = fmt.Sprintf("%s(%d)", entries[i].Name, entries[i].Value)
				data[i+10] = fmt.Sprintf("對%s宣戰", data[i])
			} else {
				data[i] = " "
				data[i+10] = " "
			}
		}
		sendHypertextWithData(sess, objID, "y_c_1", data)

	case 80027: // 財富風雲榜 — 直接顯示財富 TOP10
		entries := deps.Ranking.GetWealthRanking()
		data := formatRankingData(entries, 10)
		sendHypertextWithData(sess, objID, "y_w_1", data)

	case 80026: // 殺手風雲榜 — 顯示選單 + 等級限制
		// Java: ConfigKill.KILLLEVEL 為殺人榜上榜最低等級
		data := []string{"1"} // killLevel 暫設 1（可由 config 控制）
		sendHypertextWithData(sess, objID, "y_k_1", data)
	}
}

// handleRankingNpcAction 處理排名 NPC 選項回應（C_NPCAction）。
// Java: Npc_*Ranking.action(pc, npc, cmd, 0)
func handleRankingNpcAction(sess *net.Session, player *world.PlayerInfo, objID int32, npc *world.NpcInfo, action string, deps *Deps) bool {
	if deps.Ranking == nil {
		return false
	}

	switch npc.NpcID {
	case 80029: // 英雄風雲榜 — 依職業顯示排名
		return handleHeroRankingAction(sess, objID, action, deps)

	case 80026: // 殺手風雲榜 — 擊殺/死亡排行
		return handleKillerRankingAction(sess, objID, action, deps)

	case 80028: // 血盟風雲榜 — 宣戰
		return handleClanRankingAction(sess, player, objID, action, deps)

	default:
		return false
	}
}

// handleHeroRankingAction 英雄排名職業選擇。
// Java: Npc_HeroRanking.action() — cmd: c/k/e/w/d/g/i/wa/a
func handleHeroRankingAction(sess *net.Session, objID int32, action string, deps *Deps) bool {
	// 職業字母 → classType 映射
	classMap := map[string]int{
		"c":  0, // 王族 Crown
		"k":  1, // 騎士 Knight
		"e":  2, // 精靈 Elf
		"w":  3, // 法師 Wizard
		"d":  4, // 黑暗精靈 DarkElf
		"g":  5, // 龍騎士 DragonKnight
		"i":  6, // 幻術師 Illusionist
		"wa": 7, // 戰士 Warrior
	}

	var entries []RankingEntry
	if action == "a" {
		// 全職業 TOP10
		entries = deps.Ranking.GetHeroAll()
	} else if ct, ok := classMap[action]; ok {
		// 指定職業 TOP3
		entries = deps.Ranking.GetHeroClass(ct)
	} else {
		return false
	}

	data := formatRankingData(entries, 10)
	sendHypertextWithData(sess, objID, "y_h_2", data)
	return true
}

// handleKillerRankingAction 殺手排名選擇。
// Java: Npc_KillerRanking.action() — cmd: "1"=擊殺, "2"=死亡
func handleKillerRankingAction(sess *net.Session, objID int32, action string, deps *Deps) bool {
	switch action {
	case "1": // 殺手排行榜
		entries := deps.Ranking.GetKillRanking()
		data := formatRankingData(entries, 10)
		sendHypertextWithData(sess, objID, "y_k_2", data)
		return true

	case "2": // 死者排行榜
		entries := deps.Ranking.GetDeathRanking()
		data := formatRankingData(entries, 10)
		sendHypertextWithData(sess, objID, "y_k_3", data)
		return true

	default:
		return false
	}
}

// handleClanRankingAction 血盟排名宣戰處理。
// Java: Npc_ClanRanking.action() — cmd: "0"-"9" = 對排名第 N 的血盟宣戰
func handleClanRankingAction(sess *net.Session, player *world.PlayerInfo, objID int32, action string, deps *Deps) bool {
	idx, err := strconv.Atoi(action)
	if err != nil || idx < 0 || idx > 9 {
		return false
	}

	// 宣戰功能需要完整的戰爭系統支援（暫存 stub）
	// Java: 檢查是否為盟主 → 檢查目標血盟存在 → 發送宣戰 Y/N → 等待回應
	SendServerMessage(sess, 673) // "此功能尚未開放"
	return true
}

// formatRankingData 將排名資料格式化為固定長度的字串陣列。
// 空位填入 " "（空格），確保客戶端 HTML 顯示正確。
func formatRankingData(entries []RankingEntry, size int) []string {
	data := make([]string, size)
	for i := 0; i < size; i++ {
		if i < len(entries) {
			data[i] = fmt.Sprintf("%s,%d", entries[i].Name, entries[i].Value)
		} else {
			data[i] = " "
		}
	}
	return data
}
