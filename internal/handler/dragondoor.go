package handler

// 龍門系統（DragonDoor）— handler 層薄封裝。
// Java 參考：DragonKey.java、C_Windows.java（case 6）、S_DragonDoor.java
//
// 流程：
//   物品 47010（龍之鑰匙）使用 → 驗證地圖 + 計算可用數 → 發送 S_DragonDoor UI
//   C_Windows type=6 → 消耗鑰匙 → 委派 DragonDoorManager 生成門衛 NPC
//
// 三種門衛 NPC：
//   70932 安塔瑞斯門衛（雷歐）— 走到橋位置後開橋
//   70937 法利昂門衛（紅雷歐）— 走到橋位置後開橋
//   70934 林德拜爾門衛（火燒雷歐）— 被擊殺後開橋

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// 龍門常數
const (
	dragonKeyItemID  int32 = 47010 // 龍之鑰匙物品 ID
	keeperAntharas   int32 = 70932 // 安塔瑞斯門衛 NPC ID
	keeperFafurion   int32 = 70937 // 法利昂門衛 NPC ID
	keeperLindvior   int32 = 70934 // 林德拜爾門衛 NPC ID
	keeperMaxPerType       = 6     // 每種門衛最大數量
	keeperLifetimeSec      = 7200  // 門衛存活時間（秒）

	msgCannotUseHere int = 1892 // 此處無法使用
)

// selectDoor → NPC ID 對應表
var dragonDoorNpcMap = [3]int32{
	0: keeperAntharas, // 安塔瑞斯
	1: keeperFafurion, // 法利昂
	2: keeperLindvior, // 林德拜爾
}

// HandleDragonKeyUse 處理龍之鑰匙（物品 47010）使用。
// 由 handleUseEtcItem 中的物品路由觸發。
// Java: DragonKey.execute() — 驗證地圖 + 攻城戰 → 計算可用門衛數 → 發送 S_DragonDoor。
func HandleDragonKeyUse(sess *net.Session, player *world.PlayerInfo, invItem *world.InvItem, deps *Deps) {
	// 攻城戰區域檢查（Java: L1CastleLocation.getCastleIdByArea + isNowWar）
	// TODO: 城堡戰爭系統實裝後補上
	// if castleID > 0 && isWarActive(castleID) { sendServerMessage(sess, uint16(msgCannotUseHere)); return }

	// 地圖白名單檢查（Java: ConfigOtherSet2.DRAGON_KEY_MAP_LIST）
	if !isDragonKeyMap(player.MapID) {
		sendServerMessage(sess, uint16(msgCannotUseHere))
		return
	}

	// 計算各類型門衛可用數量
	a, b, c := keeperMaxPerType, keeperMaxPerType, keeperMaxPerType
	if deps.DragonDoor != nil {
		a, b, c = deps.DragonDoor.GetAvailableCounts()
	}

	// 發送 S_DragonDoor 封包（客戶端顯示龍門選擇 UI）
	sendDragonDoor(sess, invItem.ObjectID, a, b, c, 0)
}

// HandleDragonDoorSelect 處理 C_Windows type=6（龍門選擇）。
// Java: C_Windows.java case 6 — readD(itemObjID) + readD(selectDoor)
// → 驗證物品 → 消耗鑰匙 → 生成門衛 NPC。
func HandleDragonDoorSelect(sess *net.Session, player *world.PlayerInfo, r *packet.Reader, deps *Deps) {
	itemObjID := r.ReadD()
	selectDoor := r.ReadD()

	// 驗證物品仍在背包中
	invItem := player.Inv.FindByObjectID(itemObjID)
	if invItem == nil || invItem.ItemID != dragonKeyItemID {
		return
	}

	// 第四項未實裝
	if selectDoor == 3 {
		SendSystemMessage(sess, "該副本尚未實裝。")
		return
	}

	// 有效選項：0/1/2
	if selectDoor < 0 || selectDoor > 2 {
		return
	}

	if deps.DragonDoor == nil {
		return
	}

	// 消耗 1 把龍之鑰匙
	deps.NpcSvc.ConsumeItem(sess, player, invItem.ObjectID, 1)

	// 委派 DragonDoorSystem 生成門衛 NPC
	npcID := dragonDoorNpcMap[selectDoor]
	deps.DragonDoor.SpawnKeeper(sess, player, npcID)
}

// isDragonKeyMap 檢查地圖是否允許使用龍之鑰匙。
// Java: ConfigOtherSet2.DRAGON_KEY_MAP_LIST — 可設定的地圖白名單。
// 龍窟副本地圖：安塔瑞斯(1005)、法利昂(1011)、林德拜爾(1017)。
func isDragonKeyMap(mapID int16) bool {
	switch mapID {
	case 1005, 1011, 1017:
		return true
	}
	return false
}

// sendDragonDoor 發送 S_DragonDoor 封包（S_PacketBox subtype 102）。
// Java: S_DragonDoor.java — [C 250][C 102][D itemObjID][C a][C b][C c][C d]
func sendDragonDoor(sess *net.Session, itemObjID int32, a, b, c, d int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(102)
	w.WriteD(itemObjID)
	w.WriteC(byte(a))
	w.WriteC(byte(b))
	w.WriteC(byte(c))
	w.WriteC(byte(d))
	sess.Send(w.Bytes())
}
