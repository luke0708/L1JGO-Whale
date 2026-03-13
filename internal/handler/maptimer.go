package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// ═══════════════════════════════════════════════════════════════
// 地圖定時器（MapTimer）系統
// Java: MapTimerThread.java, S_MapTimer.java, S_MapTimerOut.java
// ═══════════════════════════════════════════════════════════════

// MapTimerGroup 定義一組限時地圖。
// Java: MapsGroupTable / mapids_group 資料表。
type MapTimerGroup struct {
	OrderID    int     // 組別 ID（1-based，用於 DB 持久化）
	Name       string  // 顯示名稱（Ctrl+Q 用）
	MaxTimeSec int     // 最大停留時間（秒）
	MapIDs     []int16 // 屬於此組的所有地圖 ID
	ExitX      int32   // 時間到時的傳送目標 X
	ExitY      int32   // 時間到時的傳送目標 Y
	ExitMapID  int16   // 時間到時的傳送目標地圖
	ExitHead   int16   // 時間到時的傳送面向
}

// mapTimerGroups 所有限時地圖組（對照 Java mapids_group 資料表）。
// 地圖 ID 必須與 map_list.yaml 一致。
var mapTimerGroups = []MapTimerGroup{
	{
		OrderID: 1, Name: "龍之谷地監", MaxTimeSec: 7200,
		MapIDs:  []int16{30, 31, 32, 33, 35, 36},
		ExitX:   32628, ExitY: 32773, ExitMapID: 4, ExitHead: 5,
	},
	{
		OrderID: 2, Name: "古魯丁地監", MaxTimeSec: 7200,
		MapIDs:  []int16{7, 8, 9, 10, 11, 12, 13},
		ExitX:   32611, ExitY: 32820, ExitMapID: 4, ExitHead: 5,
	},
	{
		OrderID: 3, Name: "奇岩地監", MaxTimeSec: 7200,
		MapIDs:  []int16{53, 54, 55, 56},
		ExitX:   33433, ExitY: 32812, ExitMapID: 4, ExitHead: 5,
	},
	{
		OrderID: 4, Name: "象牙塔", MaxTimeSec: 7200,
		MapIDs:  []int16{75, 76, 77, 78, 79, 80, 81, 82},
		ExitX:   33443, ExitY: 32800, ExitMapID: 4, ExitHead: 5,
	},
	{
		OrderID: 5, Name: "傲慢之塔", MaxTimeSec: 7200,
		MapIDs:  generateMapRange(101, 200, 301),
		ExitX:   33443, ExitY: 32800, ExitMapID: 4, ExitHead: 5,
	},
	{
		OrderID: 6, Name: "拉斯塔巴德地監", MaxTimeSec: 7200,
		MapIDs:  []int16{307, 308, 309, 450},
		ExitX:   33443, ExitY: 32800, ExitMapID: 4, ExitHead: 5,
	},
}

// generateMapRange 生成 [from..to] 連續地圖 ID + 額外 ID。
func generateMapRange(from, to int16, extra ...int16) []int16 {
	ids := make([]int16, 0, int(to-from+1)+len(extra))
	for i := from; i <= to; i++ {
		ids = append(ids, i)
	}
	ids = append(ids, extra...)
	return ids
}

// mapToGroupIdx 地圖 ID → mapTimerGroups 索引（快速查找用）。
var mapToGroupIdx map[int16]int

func init() {
	mapToGroupIdx = make(map[int16]int)
	for i, g := range mapTimerGroups {
		for _, mid := range g.MapIDs {
			mapToGroupIdx[mid] = i
		}
	}
}

// GetMapTimerGroup 返回指定地圖所屬的限時地圖組，不存在則返回 nil。
func GetMapTimerGroup(mapID int16) *MapTimerGroup {
	idx, ok := mapToGroupIdx[mapID]
	if !ok {
		return nil
	}
	return &mapTimerGroups[idx]
}

// --- 傳送門進入限時地圖時的處理 ---

// OnEnterTimedMap 玩家進入限時地圖時呼叫。委派給 MapTimerManager。
func OnEnterTimedMap(sess *net.Session, player *world.PlayerInfo, mapID int16, deps *Deps) {
	if deps.MapTimer != nil {
		deps.MapTimer.OnEnterTimedMap(player, mapID)
	}
}

// SendMapTimer 發送 S_PacketBox(MAP_TIMER=153) — 左上角限時倒計時。
// Java: S_MapTimer — [C 250][C 153][H 剩餘秒數]
func SendMapTimer(sess *net.Session, remainingSeconds int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT) // 250
	w.WriteC(153)                                           // MAP_TIMER
	w.WriteH(uint16(remainingSeconds))
	sess.Send(w.Bytes())
}

// --- Ctrl+Q 查看限時地圖剩餘時間 ---

// SendMapTimerOut 發送 S_PacketBox(DISPLAY_MAP_TIME=159) — Ctrl+Q 顯示所有限時地圖剩餘時間。
// Java: S_MapTimerOut / S_PacketBoxMapTimer — [C 250][C 159][D 組數]{[D orderID][S 名稱][D 剩餘分鐘]}...
func SendMapTimerOut(sess *net.Session, player *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT) // 250
	w.WriteC(159)                                           // DISPLAY_MAP_TIME
	w.WriteD(int32(len(mapTimerGroups)))

	for _, grp := range mapTimerGroups {
		var usedSec int
		if player.MapTimeUsed != nil {
			usedSec = player.MapTimeUsed[grp.OrderID]
		}
		remainMin := (grp.MaxTimeSec - usedSec) / 60
		if remainMin < 0 {
			remainMin = 0
		}
		w.WriteD(int32(grp.OrderID))
		w.WriteS(grp.Name)
		w.WriteD(int32(remainMin))
	}
	sess.Send(w.Bytes())
}
