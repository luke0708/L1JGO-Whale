package handler

import (
	"fmt"
	"time"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// --- 旅館系統 ---
// Java: C_NPCAction.java — "room", "hall", "return", "enter" 動作
// Java: C_Amount.java — 旅館租房金額回應
// Java: InnTable / InnKeyTable / L1Inn / S_HowManyKey

// innKeyItemID 旅館鑰匙物品 ID。
const innKeyItemID int32 = 40312

// innPricePerKey 每把鑰匙價格（金幣）。
const innPricePerKey int32 = 300

// innRentalDuration 租約持續時間（4 小時）。
// Java: System.currentTimeMillis() + (60 * 60 * 4 * 1000)
const innRentalDuration = 4 * time.Hour

// innRoomCoords 旅館 NPC ID → 房間/會議室傳送座標。
// Java: C_NPCAction.java enter 動作的 switch(npcId)
// 格式：{roomX, roomY, roomMapID, hallX, hallY, hallMapID}
var innRoomCoords = map[int32][6]int32{
	70012: {32745, 32803, 16384, 32743, 32808, 16896}, // 說話之島
	70019: {32743, 32803, 17408, 32744, 32807, 17920}, // 古魯丁
	70031: {32744, 32803, 18432, 32744, 32807, 18944}, // 奇岩
	70065: {32744, 32803, 19456, 32744, 32807, 19968}, // 歐瑞
	70070: {32744, 32803, 20480, 32744, 32807, 20992}, // 風木
	70075: {32744, 32803, 21504, 32744, 32807, 22016}, // 銀騎士
	70084: {32744, 32803, 22528, 32744, 32807, 23040}, // 海音
	70054: {32744, 32803, 23552, 32744, 32807, 24064}, // 亞丁
	70096: {32744, 32803, 24576, 32744, 32807, 25088}, // 海賊島
}

// isInnNpc 檢查 NPC 是否為旅館 NPC。
func isInnNpc(npcID int32) bool {
	_, ok := innRoomCoords[npcID]
	return ok
}

// handleInnAction 處理旅館相關的 NPC 動作。回傳 true 表示已處理。
func handleInnAction(sess *net.Session, player *world.PlayerInfo, objID int32, npcID int32, action string, deps *Deps) bool {
	if !isInnNpc(npcID) {
		return false
	}
	switch action {
	case "room":
		handleInnRoom(sess, player, objID, npcID, deps)
		return true
	case "hall":
		handleInnHall(sess, player, objID, npcID, deps)
		return true
	case "return":
		handleInnReturn(sess, player, objID, npcID, deps)
		return true
	case "enter":
		handleInnEnter(sess, player, npcID, deps)
		return true
	}
	return false
}

// handleInnRoom 處理 "room" 動作 — 租用一般房間。
// Java: C_NPCAction.java line 515-565
func handleInnRoom(sess *net.Session, player *world.PlayerInfo, npcObjID, npcID int32, deps *Deps) {
	rooms := deps.InnRooms[npcID]
	if rooms == nil {
		return
	}

	now := time.Now()
	canRent := false
	findRoom := false
	isRented := false
	isHall := false
	roomNumber := int32(0)
	roomCount := int32(0)

	for i := int32(0); i < 16; i++ {
		room := rooms[i]
		if room == nil {
			continue
		}
		if room.Hall {
			isHall = true
		}
		expired := now.After(room.DueTime) || now.Equal(room.DueTime)

		// 玩家已租此房間且未到期
		if room.LodgerID == player.CharID && !expired {
			isRented = true
			break
		}
		// 玩家身上有旅館鑰匙
		if playerHasInnKey(player, npcID) {
			isRented = true
			break
		}
		if !findRoom && !isRented {
			if expired {
				canRent = true
				findRoom = true
				roomNumber = room.RoomNumber
			} else if !room.Hall {
				roomCount++
			}
		}
	}

	if isRented {
		if isHall {
			sendHypertext(sess, npcObjID, "inn15")
		} else {
			sendHypertext(sess, npcObjID, "inn5")
		}
	} else if roomCount >= 12 {
		sendHypertext(sess, npcObjID, "inn6")
	} else if canRent {
		player.PendingInnNpcObjID = npcObjID
		player.PendingInnRoomNum = roomNumber
		player.PendingInnHall = false
		sendInnKeyDialog(sess, npcObjID, 300, 1, 8, "inn2", deps.World.GetNpc(npcObjID))
	}
}

// handleInnHall 處理 "hall" 動作 — 租用會議室（僅君主）。
// Java: C_NPCAction.java line 566-620
func handleInnHall(sess *net.Session, player *world.PlayerInfo, npcObjID, npcID int32, deps *Deps) {
	// 必須是君主（ClassType 0 = 王族）
	if player.ClassType != 0 {
		sendHypertext(sess, npcObjID, "inn10")
		return
	}

	rooms := deps.InnRooms[npcID]
	if rooms == nil {
		return
	}

	now := time.Now()
	canRent := false
	findRoom := false
	isRented := false
	isHall := false
	roomNumber := int32(0)
	roomCount := int32(0)

	for i := int32(0); i < 16; i++ {
		room := rooms[i]
		if room == nil {
			continue
		}
		if room.Hall {
			isHall = true
		}
		expired := now.After(room.DueTime) || now.Equal(room.DueTime)

		if room.LodgerID == player.CharID && !expired {
			isRented = true
			break
		}
		if playerHasInnKey(player, npcID) {
			isRented = true
			break
		}
		if !findRoom && !isRented {
			if expired {
				canRent = true
				findRoom = true
				roomNumber = room.RoomNumber
			} else if room.Hall {
				roomCount++
			}
		}
	}

	if isRented {
		if isHall {
			sendHypertext(sess, npcObjID, "inn15")
		} else {
			sendHypertext(sess, npcObjID, "inn5")
		}
	} else if roomCount >= 4 {
		sendHypertext(sess, npcObjID, "inn16")
	} else if canRent {
		player.PendingInnNpcObjID = npcObjID
		player.PendingInnRoomNum = roomNumber
		player.PendingInnHall = true
		sendInnKeyDialog(sess, npcObjID, 300, 1, 16, "inn12", deps.World.GetNpc(npcObjID))
	}
}

// handleInnReturn 處理 "return" 動作 — 退租。委派給 InnSystem。
func handleInnReturn(sess *net.Session, player *world.PlayerInfo, npcObjID, npcID int32, deps *Deps) {
	if deps.Inn != nil {
		deps.Inn.ReturnRoom(sess, player, npcObjID, npcID)
	}
}

// handleInnEnter 處理 "enter" 動作 — 使用鑰匙進入房間。
// Java: C_NPCAction.java line 665-733
func handleInnEnter(sess *net.Session, player *world.PlayerInfo, npcID int32, deps *Deps) {
	coords, ok := innRoomCoords[npcID]
	if !ok {
		return
	}

	rooms := deps.InnRooms[npcID]
	now := time.Now()

	for _, item := range player.Inv.Items {
		if item.InnNpcID != npcID {
			continue
		}
		// 找到對應房間
		if rooms != nil {
			for i := int32(0); i < 16; i++ {
				room := rooms[i]
				if room == nil || room.KeyID != item.InnKeyID {
					continue
				}
				// 檢查租約是否到期
				dueTime := time.Unix(item.InnDueTime, 0)
				if now.Before(dueTime) {
					// 傳送到房間
					if !item.InnHall {
						teleportPlayer(sess, player, coords[0], coords[1], int16(coords[2]), 6, deps)
					} else {
						teleportPlayer(sess, player, coords[3], coords[4], int16(coords[5]), 6, deps)
					}
					return
				}
			}
		}
	}
}

// HandleInnRental 處理 C_Amount (opcode 11) 中旅館租房回應。
// Handler 只做解析 + 驗證 + 委派給 InnSystem。
func HandleInnRental(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	npcObjID := player.PendingInnNpcObjID
	player.PendingInnNpcObjID = 0

	_ = r.ReadD()       // objectId
	amount := r.ReadD() // 鑰匙數量
	_ = r.ReadC()       // unknown
	_ = r.ReadS()       // action string

	if amount <= 0 {
		return
	}

	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		return
	}
	if !isInnNpc(npc.NpcID) {
		return
	}

	if deps.Inn != nil {
		deps.Inn.RentRoom(sess, player, npcObjID, npc.NpcID, amount)
	}
}

// playerHasInnKey 檢查玩家是否持有指定旅館 NPC 的鑰匙。
func playerHasInnKey(player *world.PlayerInfo, npcID int32) bool {
	for _, item := range player.Inv.Items {
		if item.ItemID == innKeyItemID && item.InnNpcID == npcID {
			return true
		}
	}
	return false
}

// sendInnKeyDialog 發送 S_HowManyKey 封包（opcode 136 = S_OPCODE_INPUTAMOUNT）。
// Java: S_HowManyKey.java — 旅館鑰匙數量選擇對話框。
// 格式與 S_HowManyMake（製作）和 S_ApplyAuction（拍賣）共用 opcode 但結構不同。
func sendInnKeyDialog(sess *net.Session, npcObjID, price, min, max int32, htmlID string, npc *world.NpcInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_INPUTAMOUNT)
	w.WriteD(npcObjID)
	w.WriteD(price) // 每日價格
	w.WriteD(min)   // 最小值（同時作為初始值）
	w.WriteD(min)   // 最小值（重複，Java: writeD(min) × 2）
	w.WriteD(max)   // 最大值
	w.WriteH(0)     // unknown

	w.WriteS(htmlID) // HTML ID（客戶端 UI 狀態識別）
	w.WriteC(0)      // 分隔符
	w.WriteH(2)      // 後續資料字串數量
	npcName := ""
	if npc != nil {
		npcName = npc.Name
	}
	w.WriteS(npcName)                    // NPC 名稱
	w.WriteS(fmt.Sprintf("%d", price)) // 價格字串

	sess.Send(w.Bytes())
}
