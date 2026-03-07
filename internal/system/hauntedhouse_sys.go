package system

// 鬼屋副本系統（幽靈之家）
// Java 參考：L1HauntedHouse.java、L1FieldObjectInstance.java（NPC 81171）
//
// 狀態機：STATUS_NONE → STATUS_READY(90秒) → STATUS_PLAYING(300秒) → STATUS_NONE
// 勝者名額：1-4人=1名、5-7人=2名、8-10人=3名
// 計時器以 tick 計數驅動（5 ticks ≈ 1 秒），Phase 3（PostUpdate）

import (
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// 狀態常數
const (
	hhStatusNone    = 0
	hhStatusReady   = 1
	hhStatusPlaying = 2
)

// 計時常數（單位：tick，5 ticks ≈ 1 秒）
const (
	hhReadyTicks   = 450  // 90 秒準備期
	hhPlayingTicks = 1500 // 300 秒（5 分鐘）遊戲期
)

// HauntedHouseSystem 鬼屋副本系統。
// 實作 handler.HauntedHouseManager 介面。
type HauntedHouseSystem struct {
	deps    *handler.Deps
	ws      *world.State
	status  int
	members []*hauntedMember // 參加者列表
	winners int              // 勝者名額
	goals   int              // 已到達終點人數
	timer   int              // 計時 tick 計數器
}

type hauntedMember struct {
	charID int32
	sessID uint64
}

func NewHauntedHouseSystem(ws *world.State, deps *handler.Deps) *HauntedHouseSystem {
	return &HauntedHouseSystem{
		deps: deps,
		ws:   ws,
	}
}

func (s *HauntedHouseSystem) Phase() coresys.Phase { return coresys.PhasePostUpdate }

func (s *HauntedHouseSystem) Update(_ time.Duration) {
	if s.status == hhStatusNone {
		return
	}

	s.timer++

	switch s.status {
	case hhStatusReady:
		if s.timer >= hhReadyTicks {
			s.startGame()
		}
	case hhStatusPlaying:
		// 移除已離開地圖的成員（Java: removeRetiredMembers）
		s.removeRetiredMembers()

		if s.timer >= hhPlayingTicks {
			s.endGame()
		}
	}
}

// ==================== HauntedHouseManager 介面實作 ====================

// AddMember 嘗試加入鬼屋副本。
// Java: C_NPCAction.enterHauntedHouse() — 狀態/人數驗證 → 傳送到等候室。
func (s *HauntedHouseSystem) AddMember(sess *net.Session, player *world.PlayerInfo) {
	// 遊戲進行中拒絕
	if s.status == hhStatusPlaying {
		handler.SendServerMessage(sess, uint16(1182))
		return
	}

	// 人數已滿
	if len(s.members) >= 10 {
		handler.SendServerMessage(sess, uint16(1184))
		return
	}

	// 已是成員則忽略
	if s.isMember(player.CharID) {
		return
	}

	s.members = append(s.members, &hauntedMember{
		charID: player.CharID,
		sessID: sess.ID,
	})

	// 傳送到鬼屋地圖（Java: L1Teleport.teleport(pc, 32722, 32830, 5140, 2, true)）
	handler.TeleportPlayer(sess, player, 32722, 32830, 5140, 2, s.deps)

	// 第一人加入且狀態為 NONE → 進入準備階段
	if len(s.members) == 1 && s.status == hhStatusNone {
		s.status = hhStatusReady
		s.timer = 0
		s.deps.Log.Info("鬼屋副本：進入準備階段（90 秒）")
	}
}

// OnGoalReached 處理玩家觸碰終點鬼火（NPC 81171）。
// Java: L1FieldObjectInstance.onAction() — 判定勝者、給獎品、傳送出去。
func (s *HauntedHouseSystem) OnGoalReached(sess *net.Session, player *world.PlayerInfo) {
	if s.status != hhStatusPlaying {
		return
	}

	if !s.isMember(player.CharID) {
		return
	}

	if s.goals+1 == s.winners {
		// 最後一位勝者 → 給獎品 → 結束活動
		s.GiveReward(sess, player)
		s.endGame()
	} else if s.goals+1 < s.winners {
		// 非最後勝者 → 給獎品 → 移出 → 傳送
		s.goals++
		s.removeMember(player.CharID)

		s.GiveReward(sess, player)

		// 清 buff → 傳送出去
		if s.deps.Skill != nil {
			s.deps.Skill.CancelAllBuffs(player)
		}
		handler.TeleportPlayer(sess, player, 32624, 32813, 4, 5, s.deps)
	}
}

// RemoveOnDisconnect 玩家斷線時移除。
func (s *HauntedHouseSystem) RemoveOnDisconnect(player *world.PlayerInfo) {
	s.removeMember(player.CharID)
}

// ==================== 內部邏輯 ====================

// startGame 開始鬼屋遊戲。
// Java: L1HauntedHouse.startHauntedHouse()
func (s *HauntedHouseSystem) startGame() {
	s.status = hhStatusPlaying
	s.timer = 0

	// 計算勝者名額（Java: 1-4人=1名、5-7人=2名、8-10人=3名）
	count := len(s.members)
	switch {
	case count <= 4:
		s.winners = 1
	case count <= 7:
		s.winners = 2
	default:
		s.winners = 3
	}
	s.goals = 0

	s.deps.Log.Info("鬼屋副本：遊戲開始",
		zap.Int("參加人數", count),
		zap.Int("勝者名額", s.winners),
	)

	// 對所有成員：清 buff → 變身 GFX 6284（300 秒）
	for _, m := range s.members {
		p := s.ws.GetByCharID(m.charID)
		if p == nil {
			continue
		}

		// 清除所有 buff（Java: CANCELLATION）
		if s.deps.Skill != nil {
			s.deps.Skill.CancelAllBuffs(p)
		}

		// 變身為鬼魂外觀（Java: L1PolyMorph.doPoly(pc, 6284, 300, MORPH_BY_NPC)）
		if s.deps.Polymorph != nil {
			s.deps.Polymorph.DoPoly(p, 6284, 300, data.PolyCauseNPC)
		}
	}

	// 開啟地圖 5140 上所有門（Java: 遍歷 World 中所有門，開啟 mapId==5140 的）
	doors := s.ws.GetDoorsByMap(5140)
	for _, door := range doors {
		if door.Open() {
			handler.BroadcastDoorOpen(door, s.deps)
		}
	}
}

// endGame 結束鬼屋遊戲。
// Java: L1HauntedHouse.endHauntedHouse()
func (s *HauntedHouseSystem) endGame() {
	s.deps.Log.Info("鬼屋副本：遊戲結束")

	s.status = hhStatusNone
	s.winners = 0
	s.goals = 0

	// 傳送所有仍在地圖上的成員回出口
	for _, m := range s.members {
		p := s.ws.GetByCharID(m.charID)
		if p == nil || p.MapID != 5140 {
			continue
		}

		// 清 buff
		if s.deps.Skill != nil {
			s.deps.Skill.CancelAllBuffs(p)
		}

		// 傳送到地圖 4 的出口點（Java: 32624, 32813, map 4, heading 5）
		handler.TeleportPlayer(p.Session, p, 32624, 32813, 4, 5, s.deps)
	}

	// 清成員列表
	s.members = s.members[:0]

	// 關閉地圖 5140 上所有門
	doors := s.ws.GetDoorsByMap(5140)
	for _, door := range doors {
		if door.Close() {
			handler.BroadcastDoorClose(door, s.deps)
		}
	}
}

// removeRetiredMembers 移除已不在鬼屋地圖的成員。
// Java: L1HauntedHouse.removeRetiredMembers()
func (s *HauntedHouseSystem) removeRetiredMembers() {
	n := 0
	for _, m := range s.members {
		p := s.ws.GetByCharID(m.charID)
		if p != nil && p.MapID == 5140 {
			s.members[n] = m
			n++
		}
	}
	s.members = s.members[:n]
}

// isMember 檢查角色是否為鬼屋成員。
func (s *HauntedHouseSystem) isMember(charID int32) bool {
	for _, m := range s.members {
		if m.charID == charID {
			return true
		}
	}
	return false
}

// removeMember 移除指定角色。
func (s *HauntedHouseSystem) removeMember(charID int32) {
	for i, m := range s.members {
		if m.charID == charID {
			s.members = append(s.members[:i], s.members[i+1:]...)
			return
		}
	}
}

// GiveReward 給予鬼屋獎品（物品 41308 勇者的南瓜袋子）。
func (s *HauntedHouseSystem) GiveReward(sess *net.Session, player *world.PlayerInfo) {
	const rewardItemID int32 = 41308 // 勇者的南瓜袋子

	itemInfo := s.deps.Items.Get(rewardItemID)
	if itemInfo == nil {
		return
	}

	if player.Inv.IsFull() {
		return
	}

	existing := player.Inv.FindByItemID(rewardItemID)
	wasExisting := existing != nil && itemInfo.Stackable

	invItem := player.Inv.AddItem(
		rewardItemID, 1, itemInfo.Name, itemInfo.InvGfx,
		itemInfo.Weight, itemInfo.Stackable, byte(itemInfo.Bless),
	)

	if wasExisting {
		handler.SendItemCountUpdate(sess, invItem)
	} else {
		handler.SendAddItem(sess, invItem, itemInfo)
	}

	// S_ServerMessage 403: 獲得 %0
	handler.SendServerMessageStr(sess, 403, invItem.Name)
}
