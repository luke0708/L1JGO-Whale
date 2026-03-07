package handler

// 鬼屋副本（幽靈之家）— handler 層薄封裝。
// Java 參考：L1HauntedHouse.java、L1FieldObjectInstance.java、C_NPCAction.java
//
// 流程：
//   NPC 80085 動作 "ent" → enterHauntedHouse（驗證 + 加入）
//   NPC 81171 被點擊（C_Attack） → onHauntedHouseGoal（終點判定）
//   計時器由 system/hauntedhouse_sys.go 驅動

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// 鬼屋常數
const (
	hauntedHouseMapID   int16 = 5140 // 幽靈之家地圖
	hauntedHouseStartX  int32 = 32722
	hauntedHouseStartY  int32 = 32830
	hauntedHouseExitX   int32 = 32624
	hauntedHouseExitY   int32 = 32813
	hauntedHouseExitMap int16 = 4
	hauntedHousePolyGfx int32 = 6284  // 變身外觀（鬼魂）
	hauntedHousePolyDur int   = 300   // 變身持續 300 秒
	hauntedHouseMaxMbr  int   = 10    // 最大參加人數
	hauntedHouseReward  int32 = 41308 // 勇者的南瓜袋子
	hauntedHouseGoalNpc int32 = 81171 // 終點鬼火 NPC ID
	hauntedHouseNpcID   int32 = 80085 // 管理人杜烏 NPC ID

	// S_ServerMessage IDs
	msgHauntedPlaying int = 1182 // 活動進行中，無法進入
	msgHauntedFull    int = 1184 // 人數已滿
	msgGotItem        int = 403  // 獲得 %0
)

// enterHauntedHouse 嘗試讓玩家加入鬼屋副本。
// 由 HandleNpcAction "ent" + NPC 80085 觸發。
func enterHauntedHouse(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if deps.HauntedHouse == nil {
		return
	}
	deps.HauntedHouse.AddMember(sess, player)
}

// onHauntedHouseGoal 處理玩家點擊終點鬼火（NPC 81171）。
// 由 CombatSystem 的 FieldObject 分支觸發。
func onHauntedHouseGoal(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if deps.HauntedHouse == nil {
		return
	}
	deps.HauntedHouse.OnGoalReached(sess, player)
}

