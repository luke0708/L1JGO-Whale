package system

import (
	"context"
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// MapTimerSystem 每秒遞減限時地圖計時，時間到則強制傳送出地圖。
// 同時負責每日重置（Java: ServerResetMapTimer + MapTimerThread）。
// Go: Phase 3（PostUpdate），透過 tick 累加器實現每秒觸發。
type MapTimerSystem struct {
	world   *world.State
	deps    *handler.Deps
	lastDay int // 上次重置時的日期（day of year）
}

func NewMapTimerSystem(ws *world.State, deps *handler.Deps) *MapTimerSystem {
	return &MapTimerSystem{
		world:   ws,
		deps:    deps,
		lastDay: time.Now().YearDay(),
	}
}

func (s *MapTimerSystem) Phase() coresys.Phase { return coresys.PhasePostUpdate }

func (s *MapTimerSystem) Update(_ time.Duration) {
	// 每日重置檢查（Java: ServerResetMapTimer，每 24 小時執行一次）
	today := time.Now().YearDay()
	if today != s.lastDay {
		s.lastDay = today
		s.resetAllOnlinePlayers()
	}

	// 逐玩家 tick 計時
	s.world.AllPlayers(func(p *world.PlayerInfo) {
		if p.MapTimerGroupIdx <= 0 {
			return // 不在限時地圖
		}

		// tick 累加器：每 5 tick（約 1 秒）觸發一次
		p.MapTimerTickAcc++
		if p.MapTimerTickAcc < 5 {
			return
		}
		p.MapTimerTickAcc = 0

		expired := s.TickMapTimer(p)
		if expired {
			// 時間到 → 強制傳送到地圖出口
			grp := handler.GetMapTimerGroup(p.MapID)
			if grp == nil {
				// 玩家已不在限時地圖（可能被其他系統傳送）
				p.MapTimerGroupIdx = -1
				return
			}
			handler.TeleportPlayer(p.Session, p, grp.ExitX, grp.ExitY, grp.ExitMapID, grp.ExitHead, s.deps)
		}
	})
}

// resetAllOnlinePlayers 每日重置所有線上玩家的限時地圖時間 + DB 全量重置。
// Java: ServerResetMapTimer.ResetTimingMap()
func (s *MapTimerSystem) resetAllOnlinePlayers() {
	s.deps.Log.Info("每日重置限時地圖計時器")

	// 1. 重置所有線上玩家
	s.world.AllPlayers(func(p *world.PlayerInfo) {
		s.ResetAllMapTimers(p)
		// 若仍在限時地圖中，重新啟動計時器
		if grp := handler.GetMapTimerGroup(p.MapID); grp != nil {
			s.OnEnterTimedMap(p, p.MapID)
		}
	})

	// 2. DB 全量重置離線玩家
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.deps.CharRepo.ResetAllMapTimes(ctx); err != nil {
		s.deps.Log.Error("每日重置限時地圖 DB 失敗", zap.Error(err))
	}
}

// --- MapTimerManager 介面實作 ---

// OnEnterTimedMap 玩家進入限時地圖時呼叫。
// 計算剩餘時間，發送 S_MapTimer 封包，啟動計時。
// Java: Teleportation.teleportation() 中的 isTimingMap 檢查。
func (s *MapTimerSystem) OnEnterTimedMap(player *world.PlayerInfo, mapID int16) {
	grp := handler.GetMapTimerGroup(mapID)
	if grp == nil {
		// 離開限時地圖 → 停止計時
		player.MapTimerGroupIdx = -1
		return
	}

	if player.MapTimeUsed == nil {
		player.MapTimeUsed = make(map[int]int)
	}
	usedSec := player.MapTimeUsed[grp.OrderID]
	remaining := grp.MaxTimeSec - usedSec
	if remaining <= 0 {
		remaining = 0
	}

	player.MapTimerGroupIdx = grp.OrderID
	player.MapTimerRemaining = remaining

	// 發送 S_MapTimer — 客戶端左上角顯示倒計時
	handler.SendMapTimer(player.Session, remaining)
}

// TickMapTimer 每秒呼叫一次，遞減限時地圖計時。
// 返回 true 表示時間到需強制傳送。
// Java: MapTimerThread.MapTimeCheck()。
func (s *MapTimerSystem) TickMapTimer(player *world.PlayerInfo) (expired bool) {
	if player.MapTimerGroupIdx <= 0 {
		return false // 不在限時地圖中
	}
	if player.Dead {
		return false // 死亡不計時（Java: pc.isDead() → continue）
	}

	// 遞增已使用時間
	if player.MapTimeUsed == nil {
		player.MapTimeUsed = make(map[int]int)
	}
	player.MapTimeUsed[player.MapTimerGroupIdx]++
	player.MapTimerRemaining--

	if player.MapTimerRemaining <= 0 {
		return true // 時間到
	}

	// 每秒發送倒數更新（Java: CheckTimeController 每秒發送 S_MapTimer）
	handler.SendMapTimer(player.Session, player.MapTimerRemaining)
	return false
}

// ResetAllMapTimers 日結重置所有限時地圖時間。
// Java: ServerResetMapTimer.ResetTimingMap()。
func (s *MapTimerSystem) ResetAllMapTimers(player *world.PlayerInfo) {
	for k := range player.MapTimeUsed {
		delete(player.MapTimeUsed, k)
	}
	player.MapTimerRemaining = 0
	player.MapTimerGroupIdx = -1
}
