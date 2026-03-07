package handler

import (
	"fmt"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// --- 聯盟系統（Alliance） ---
// Java: L1Alliance, ClanAllianceTable, S_ChatClanAlliance
// 最多 4 個血盟組成聯盟；聯盟聊天 chatType=15

// ChatAlliance 聯盟聊天 type（Java: chatType_15）
const ChatAlliance = 15

// AllianceInfo 聯盟記憶體結構。
type AllianceInfo struct {
	OrderID  int32   // character_alliance.order_id
	ClanIDs  [4]int32 // 最多 4 個血盟 ID（0 表示空位）
}

// ClanCount 回傳聯盟中的血盟數量。
func (a *AllianceInfo) ClanCount() int {
	n := 0
	for _, id := range a.ClanIDs {
		if id != 0 {
			n++
		}
	}
	return n
}

// Contains 檢查聯盟是否包含指定血盟。
func (a *AllianceInfo) Contains(clanID int32) bool {
	for _, id := range a.ClanIDs {
		if id == clanID {
			return true
		}
	}
	return false
}

// AllianceManager 管理所有聯盟的記憶體快取。
type AllianceManager struct {
	alliances map[int32]*AllianceInfo // orderID → alliance
}

// NewAllianceManager 建構聯盟管理器。
func NewAllianceManager() *AllianceManager {
	return &AllianceManager{
		alliances: make(map[int32]*AllianceInfo),
	}
}

// AddAlliance 加入聯盟到管理器。
func (m *AllianceManager) AddAlliance(a *AllianceInfo) {
	m.alliances[a.OrderID] = a
}

// GetAllianceByClan 查找血盟所屬的聯盟。
func (m *AllianceManager) GetAllianceByClan(clanID int32) *AllianceInfo {
	for _, a := range m.alliances {
		if a.Contains(clanID) {
			return a
		}
	}
	return nil
}

// RemoveAlliance 移除聯盟。
func (m *AllianceManager) RemoveAlliance(orderID int32) {
	delete(m.alliances, orderID)
}

// handleAllianceChat 處理聯盟聊天（chatType=15）。
// Java: C_Chat.chatType_15() → S_ChatClanAlliance → 發送給聯盟全體成員
func handleAllianceChat(sess *net.Session, player *world.PlayerInfo, text string, deps *Deps) {
	if player.ClanID == 0 {
		return
	}

	if deps.Alliances == nil {
		return
	}

	alliance := deps.Alliances.GetAllianceByClan(player.ClanID)
	if alliance == nil {
		SendServerMessage(sess, 1233) // "你的血盟沒有參加聯盟"
		return
	}

	// 聊天內容格式（Java: S_ChatClanAlliance）
	clan := deps.World.Clans.GetClan(player.ClanID)
	clanName := ""
	if clan != nil {
		clanName = clan.ClanName
	}
	msg := fmt.Sprintf("[%s][%s] %s", clanName, player.Name, text)

	// 建構封包：S_SAY (opcode 81) type 15
	// Java: S_ChatClanAlliance — writeC(opcode) + writeC(0x0f) + writeD(id) + writeS(msg)
	senderID := player.CharID
	if player.Invisible {
		senderID = 0
	}

	// 發送給聯盟中所有血盟的線上成員（排除發送者）
	for _, clanID := range alliance.ClanIDs {
		if clanID == 0 {
			continue
		}
		allianceClan := deps.World.Clans.GetClan(clanID)
		if allianceClan == nil {
			continue
		}
		for charID := range allianceClan.Members {
			member := deps.World.GetByCharID(charID)
			if member != nil && member.CharID != player.CharID {
				sendAllianceChat(member.Session, senderID, msg)
			}
		}
	}

	// 也發送給自己
	sendAllianceChat(sess, senderID, msg)
}

// sendAllianceChat 發送聯盟聊天封包。
// Java: S_ChatClanAlliance → S_OPCODE_NORMALCHAT + type=15
func sendAllianceChat(sess *net.Session, senderID int32, msg string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SAY)
	w.WriteC(ChatAlliance) // chatType = 15
	w.WriteD(senderID)
	w.WriteS(msg)
	sess.Send(w.Bytes())
}

// handleAllianceQuery 處理聯盟查詢（C_Rank data=2）。
// Java: C_Rank case 2 → S_PacketBox(PLEDGE_UNION, nameString)
func handleAllianceQuery(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.ClanID == 0 {
		SendServerMessage(sess, 1233) // "你的血盟沒有參加聯盟"
		return
	}

	if deps.Alliances == nil {
		SendServerMessage(sess, 1233)
		return
	}

	alliance := deps.Alliances.GetAllianceByClan(player.ClanID)
	if alliance == nil {
		SendServerMessage(sess, 1233)
		return
	}

	// 組合聯盟中其他血盟的名稱
	var names string
	for _, clanID := range alliance.ClanIDs {
		if clanID == 0 || clanID == player.ClanID {
			continue
		}
		c := deps.World.Clans.GetClan(clanID)
		if c != nil {
			if names != "" {
				names += " "
			}
			names += c.ClanName
		}
	}

	// Java: S_PacketBox(S_PacketBox.CYCLOPEDIA_ALLY = 0x61, names)
	// S_OPCODE_EVENT(250) + type 0x61(97) + writeS(names)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteH(97) // CYCLOPEDIA_ALLY / PLEDGE_UNION
	w.WriteS(names)
	sess.Send(w.Bytes())
}

// handleAllianceInvite 處理聯盟邀請（C_Rank data=3）。
// Java: C_Rank case 3 → FaceToFace → S_Message_YN(223)
func handleAllianceInvite(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.ClanID == 0 {
		return
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.LeaderID != player.CharID {
		SendServerMessage(sess, 518) // "只有君主可以使用此功能"
		return
	}

	// 暫存 stub — 需要 FaceToFace 實作
	SendServerMessage(sess, 673) // "此功能尚未開放"
}

// handleAllianceLeave 處理退出聯盟（C_Rank data=4）。
// Java: C_Rank case 4 → S_Message_YN(1210) 確認
func handleAllianceLeave(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.ClanID == 0 {
		return
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.LeaderID != player.CharID {
		SendServerMessage(sess, 518) // "只有君主可以使用此功能"
		return
	}

	if deps.Alliances == nil {
		return
	}

	alliance := deps.Alliances.GetAllianceByClan(player.ClanID)
	if alliance == nil {
		SendServerMessage(sess, 1233) // "你的血盟沒有參加聯盟"
		return
	}

	// 暫存 stub — 需要 Y/N 確認流程
	SendServerMessage(sess, 673) // "此功能尚未開放"
}
