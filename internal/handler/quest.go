package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// handleQuestNpcTalk 處理任務 NPC 的對話（依玩家任務進度顯示不同 htmlid）。
// 回傳 true 表示已處理（是任務 NPC），false 表示非任務 NPC。
// 此函式符合薄層原則：僅讀取資料 + 發送回應封包，無遊戲狀態修改。
func handleQuestNpcTalk(sess *net.Session, player *world.PlayerInfo, objID int32, npcID int32, deps *Deps) bool {
	if deps.QuestData == nil {
		return false
	}
	dialogs := deps.QuestData.GetNpcDialogs(npcID)
	if len(dialogs) == 0 {
		return false
	}

	// 遍歷該 NPC 關聯的所有任務，找到第一個匹配的對話
	for _, d := range dialogs {
		step := player.QuestStep(d.QuestID)
		htmlID := d.GetDialogHtmlID(step)
		if htmlID != "" {
			sendHypertext(sess, objID, htmlID)
			return true
		}
	}

	return false
}

// handleQuestNpcAction 處理任務 NPC 的動作（接受/推進/完成任務）。
// 薄層：委派給 QuestSystem 執行遊戲邏輯。
// 回傳 true 表示已處理，false 表示非任務動作。
func handleQuestNpcAction(sess *net.Session, player *world.PlayerInfo, objID int32, npcID int32, action string, deps *Deps) bool {
	if deps.Quest != nil {
		return deps.Quest.ExecuteQuestAction(sess, player, objID, npcID, action)
	}
	return false
}
