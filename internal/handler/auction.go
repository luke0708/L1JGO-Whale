package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/persist"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// --- 血盟小屋拍賣系統（Auction Board） ---
// Java: Npc_AuctionBoard.java, S_AuctionBoard.java, S_ApplyAuction.java, S_AuctionBoardRead.java
// NPC 81161 (impl=L1AuctionBoard) — 拍賣佈告欄

// AuctionManager 拍賣管理介面（由 system/auction_sys.go 實作）。
type AuctionManager interface {
	// GetEntriesForTown 取得指定城鎮的拍賣列表（依 NPC 座標過濾）。
	GetEntriesForTown(npcX, npcY int32) []*persist.AuctionEntry
	// GetEntry 取得指定小屋的拍賣記錄。
	GetEntry(houseID int32) *persist.AuctionEntry
	// PlaceBid 出價（含 WAL 保護）。
	PlaceBid(sess *net.Session, player *world.PlayerInfo, houseID int32, amount int64) bool
	// IsAlreadyBidding 檢查玩家是否已在其他拍賣中出價。
	IsAlreadyBidding(charName string) bool
}

// handleAuctionTalk 處理拍賣 NPC 的對話（點擊 NPC）→ S_AuctionBoard。
// Java: Npc_AuctionBoard.talk() → S_AuctionBoard(npcObjID, houseList)
func handleAuctionTalk(sess *net.Session, npcObjID int32, npcX, npcY int32, deps *Deps) {
	if deps.Auction == nil {
		return
	}

	entries := deps.Auction.GetEntriesForTown(npcX, npcY)
	sendAuctionBoard(sess, npcObjID, entries)
}

// handleAuctionAction 處理拍賣 NPC 動作（select/apply/map）。
// Java: Npc_AuctionBoard.action(pc, npc, cmd, 0)
// cmd 格式："select,{houseId}" / "apply,{houseId}" / "map,{houseId}"
func handleAuctionAction(sess *net.Session, player *world.PlayerInfo, npcObjID int32, action string, deps *Deps) bool {
	if deps.Auction == nil {
		return false
	}

	parts := strings.SplitN(action, ",", 2)
	if len(parts) < 2 {
		return false
	}

	cmd := parts[0]
	houseID, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return false
	}

	entry := deps.Auction.GetEntry(int32(houseID))
	if entry == nil {
		return false
	}

	switch cmd {
	case "select":
		sendAuctionBoardRead(sess, npcObjID, entry)
		return true

	case "apply":
		// 驗證條件（Java: Npc_AuctionBoard.action "apply"）
		if player.ClassType != 0 { // 非王族
			SendServerMessage(sess, 518) // "只有血盟的君主才可以使用此指令"
			return true
		}
		if player.ClanID == 0 {
			SendServerMessage(sess, 518)
			return true
		}
		clan := deps.World.Clans.GetClan(player.ClanID)
		if clan == nil || clan.LeaderID != player.CharID {
			SendServerMessage(sess, 518) // 非盟主
			return true
		}
		if player.Level < 15 {
			SendServerMessage(sess, 519) // "等級15以下的王族不可以拍賣"
			return true
		}
		if clan.HasHouse != 0 {
			SendServerMessage(sess, 521) // "你已經擁有血盟小屋"
			return true
		}
		if deps.Auction.IsAlreadyBidding(player.Name) {
			SendServerMessage(sess, 523) // "你已經出價了另一間血盟小屋"
			return true
		}

		sendApplyAuction(sess, npcObjID, entry)
		player.PendingAuctionHouseID = int32(houseID)
		return true

	case "map":
		sendHouseMap(sess, npcObjID, int32(houseID))
		return true
	}

	return false
}

// HandleAuctionBid 處理出價（從 HandleHypertextInputResult 呼叫）。
// Java: C_Amount.java — "agapply {houseNumber}" 流程
// 封包格式：[D npcObjID][D amount][C unknown][S actionStr]
func HandleAuctionBid(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	pendingID := player.PendingAuctionHouseID
	player.PendingAuctionHouseID = 0

	_ = r.ReadD()        // npcObjID
	amount := r.ReadD()  // 出價金額
	_ = r.ReadC()        // unknown
	actionStr := r.ReadS() // "agapply {houseNumber}"

	deps.Log.Debug("拍賣出價",
		zap.String("player", player.Name),
		zap.Int32("amount", amount),
		zap.String("action", actionStr),
	)

	// 解析 actionStr 取得 houseID
	var houseID int32
	if strings.HasPrefix(actionStr, "agapply ") {
		if id, err := strconv.ParseInt(strings.TrimPrefix(actionStr, "agapply "), 10, 32); err == nil {
			houseID = int32(id)
		}
	}

	// 若 actionStr 解析失敗，用 pending 的 houseID
	if houseID == 0 {
		houseID = pendingID
	}
	if houseID == 0 || amount <= 0 {
		return
	}
	if deps.Auction == nil {
		return
	}

	deps.Auction.PlaceBid(sess, player, houseID, int64(amount))
}

// --- 封包建構 ---

// sendAuctionBoard 發送 S_AuctionBoard（opcode 156 = S_OPCODE_HOUSELIST）。
// Java: S_AuctionBoard.java
func sendAuctionBoard(sess *net.Session, npcObjID int32, entries []*persist.AuctionEntry) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HOUSELIST)
	w.WriteD(npcObjID)
	w.WriteH(uint16(len(entries)))

	for _, e := range entries {
		w.WriteD(e.HouseID)
		w.WriteS(e.HouseName)
		w.WriteH(uint16(e.HouseArea))
		// Java: deadline.get(Calendar.MONTH) + 1, deadline.get(Calendar.DATE)
		w.WriteC(byte(e.Deadline.Month()))
		w.WriteC(byte(e.Deadline.Day()))
		w.WriteD(int32(e.Price)) // Java: (int) price
	}

	sess.Send(w.Bytes())
}

// sendAuctionBoardRead 發送 S_AuctionBoardRead（opcode 39 = S_OPCODE_HYPERTEXT）。
// Java: S_AuctionBoardRead.java — htmlID="agsel" + 9 個資料字串
func sendAuctionBoardRead(sess *net.Session, npcObjID int32, e *persist.AuctionEntry) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HYPERTEXT)
	w.WriteD(npcObjID)
	w.WriteS("agsel")
	w.WriteS(fmt.Sprintf("%d", e.HouseID)) // house_number

	w.WriteH(9) // 後續字串數量
	w.WriteS(e.HouseName)                            // 0: 小屋名稱
	w.WriteS(e.Location + "$1195")                   // 1: 位置
	w.WriteS(fmt.Sprintf("%d", e.HouseArea))         // 2: 大小
	w.WriteS(e.OldOwner)                             // 3: 舊所有人
	w.WriteS(e.Bidder)                               // 4: 目前競標者
	w.WriteS(fmt.Sprintf("%d", e.Price))             // 5: 目前售價
	w.WriteS(fmt.Sprintf("%d", int(e.Deadline.Month()))) // 6: 月份
	w.WriteS(fmt.Sprintf("%d", e.Deadline.Day()))    // 7: 日
	w.WriteS(fmt.Sprintf("%d", e.Deadline.Hour()))   // 8: 時

	sess.Send(w.Bytes())
}

// sendApplyAuction 發送 S_ApplyAuction（opcode 136 = S_OPCODE_INPUTAMOUNT）。
// Java: S_ApplyAuction.java — 出價輸入框
func sendApplyAuction(sess *net.Session, npcObjID int32, e *persist.AuctionEntry) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_INPUTAMOUNT)
	w.WriteD(npcObjID)
	w.WriteD(0) // unknown

	if e.BidderID == 0 {
		// 無競標者：初始值 = 當前售價
		w.WriteD(int32(e.Price))
		w.WriteD(int32(e.Price))
	} else {
		// 有競標者：初始值 = 當前售價 + 1
		w.WriteD(int32(e.Price + 1))
		w.WriteD(int32(e.Price + 1))
	}

	w.WriteD(0x77359400)                              // 最高出價值 = 2,000,000,000
	w.WriteH(0)                                       // unknown
	w.WriteS("agapply")                               // HTML ID
	w.WriteS(fmt.Sprintf("agapply %d", e.HouseID))   // NPC 動作命令

	sess.Send(w.Bytes())
}

// --- 城鎮過濾 ---

// TownHouseRange 依 NPC 座標回傳該城鎮的 houseId 範圍。
// Java: S_AuctionBoard.java 硬編碼座標判定
func TownHouseRange(npcX, npcY int32) (min, max int32) {
	switch {
	case npcX == 33421 && npcY == 32823: // 奇岩
		return 262145, 262189
	case npcX == 33585 && npcY == 33235: // 海音
		return 327681, 327691
	case npcX == 33959 && npcY == 33253: // 亞丁
		return 458753, 458819
	case npcX == 32611 && npcY == 32775: // 古魯丁
		return 524289, 524294
	default:
		return 0, 0
	}
}

// sendHouseMap 發送 S_HouseMap（opcode 187）。
// Java: S_HouseMap.java — 通知客戶端顯示住宅地圖。
// 封包格式：writeC(187) + writeD(objectId) + writeD(houseNumber)
func sendHouseMap(sess *net.Session, npcObjID int32, houseID int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HOUSEMAP)
	w.WriteD(npcObjID)
	w.WriteD(houseID)
	sess.Send(w.Bytes())
}

// SendServerMsgWithParam 發送帶參數的伺服器訊息。匯出供 system 套件使用。
func SendServerMsgWithParam(sess *net.Session, msgID uint16, param string) {
	sendServerMessageArgs(sess, msgID, param)
}
