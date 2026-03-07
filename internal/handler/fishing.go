package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// --- 釣魚系統 ---
// Java: FishingPole.java, PcFishingTimer.java, FishingTable.java, C_FishClick.java

const (
	fishingPoleID int32 = 83001 // 高彈力釣竿
	fishingPoleA  int32 = 83014 // 裝上捲線器 A
	fishingPoleB  int32 = 83024 // 裝上捲線器 B
	fishingBaitID int32 = 83002 // 營養釣餌
	fishingMap    int16 = 5300
	fishingAction byte  = 71 // ACTION_Fishing
)

// IsFishingPole 檢查物品是否為釣竿。
func IsFishingPole(itemID int32) bool {
	return itemID == fishingPoleID || itemID == fishingPoleA || itemID == fishingPoleB
}

// HandleFishingPole 處理使用釣竿開始釣魚。驗證後委派給 FishingSystem。
func HandleFishingPole(sess *net.Session, player *world.PlayerInfo, item *world.InvItem, deps *Deps) {
	if player.MapID != fishingMap {
		SendServerMessage(sess, 1135) // "這裡無法釣魚"
		return
	}
	if player.Fishing {
		return
	}
	if player.Inv == nil || player.Inv.FindByItemID(fishingBaitID) == nil {
		SendServerMessage(sess, 1136) // "你沒有釣魚餌"
		return
	}
	if deps.Fishing != nil {
		deps.Fishing.StartFishing(player, item)
	}
}

// HandleFishClick 處理 C_FishClick（opcode 62）— 取消釣魚。
func HandleFishClick(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || !player.Fishing {
		return
	}
	if deps.Fishing != nil {
		deps.Fishing.StopFishing(player, true)
	}
}

// FinishFishing 結束釣魚狀態。委派給 FishingSystem。
func FinishFishing(player *world.PlayerInfo, sendMsg bool, deps *Deps) {
	if deps.Fishing != nil {
		deps.Fishing.StopFishing(player, sendMsg)
	}
}

// TickFishing 每 tick 更新釣魚計時器。委派給 FishingSystem。
func TickFishing(player *world.PlayerInfo, deps *Deps) {
	if deps.Fishing != nil {
		deps.Fishing.Tick(player)
	}
}

// --- 封包建構函式 ---

// SendFishingAction 發送釣魚動作封包。Exported for system package usage.
func SendFishingAction(sess *net.Session, player *world.PlayerInfo, fishX, fishY int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ACTION)
	w.WriteD(player.CharID)
	w.WriteC(fishingAction)
	w.WriteH(uint16(fishX))
	w.WriteH(uint16(fishY))
	sess.Send(w.Bytes())
}
