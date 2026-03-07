package system

import (
	"fmt"
	"sort"
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
)

// 英雄排名更新間隔（Java: 每 10 分鐘 = 600 秒 = 3000 ticks）
const rankingUpdateTicks = 3000

// 排名類別常數（Java: RankingHeroTimer, RankingClanTimer, RankingKillTimer, RankingWealthTimer）
const (
	RankingMax      = 10 // TOP10
	RankingClassMax = 3  // 各職業 TOP3
)

// RankingSystem 每 10 分鐘更新四大排名：英雄/血盟/擊殺/財富。
type RankingSystem struct {
	deps    *handler.Deps
	ws      *world.State
	elapsed int

	// 英雄排名（等級）
	heroNames map[string]bool       // 所有上榜的玩家名稱（快速查詢用）
	heroAll   [RankingMax]handler.RankingEntry   // 全職業 TOP10
	heroClass [8][RankingClassMax]handler.RankingEntry // 各職業 TOP3（0=王族..7=戰士）

	// 血盟排名（線上人數）
	clanRanking [RankingMax]handler.RankingEntry

	// 擊殺排名 / 死亡排名
	killRanking  [RankingMax]handler.RankingEntry
	deathRanking [RankingMax]handler.RankingEntry

	// 財富排名（金幣數量）
	wealthRanking [RankingMax]handler.RankingEntry
}

// NewRankingSystem 建構排名系統。
func NewRankingSystem(ws *world.State, deps *handler.Deps) *RankingSystem {
	return &RankingSystem{
		deps:      deps,
		ws:        ws,
		heroNames: make(map[string]bool),
	}
}

func (s *RankingSystem) Phase() coresys.Phase { return coresys.PhasePostUpdate }

func (s *RankingSystem) Update(_ time.Duration) {
	s.elapsed++
	if s.elapsed < rankingUpdateTicks {
		return
	}
	s.elapsed = 0
	s.recalculate()
}

// --- 排名結果查詢 API（供 NPC handler 使用）---

// IsHero 檢查玩家是否在英雄排名中。
func (s *RankingSystem) IsHero(name string) bool {
	return s.heroNames[name]
}

// GetHeroAll 取得全職業 TOP10。
func (s *RankingSystem) GetHeroAll() []handler.RankingEntry {
	return trimEntries(s.heroAll[:])
}

// GetHeroClass 取得指定職業 TOP3（classType: 0=王族..7=戰士）。
func (s *RankingSystem) GetHeroClass(classType int) []handler.RankingEntry {
	if classType < 0 || classType >= len(s.heroClass) {
		return nil
	}
	return trimEntries(s.heroClass[classType][:])
}

// GetClanRanking 取得血盟 TOP10。
func (s *RankingSystem) GetClanRanking() []handler.RankingEntry {
	return trimEntries(s.clanRanking[:])
}

// GetKillRanking 取得擊殺 TOP10。
func (s *RankingSystem) GetKillRanking() []handler.RankingEntry {
	return trimEntries(s.killRanking[:])
}

// GetDeathRanking 取得死亡 TOP10。
func (s *RankingSystem) GetDeathRanking() []handler.RankingEntry {
	return trimEntries(s.deathRanking[:])
}

// GetWealthRanking 取得財富 TOP10。
func (s *RankingSystem) GetWealthRanking() []handler.RankingEntry {
	return trimEntries(s.wealthRanking[:])
}

// FormatEntries 將排名資料格式化為 "name,value" 字串陣列（供 NPC HTML 顯示）。
func FormatEntries(entries []handler.RankingEntry) []string {
	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = fmt.Sprintf("%s,%d", e.Name, e.Value)
	}
	return result
}

// --- 內部排名計算 ---

type rankedPlayer struct {
	Name      string
	Level     int16
	ClassType int16
	KillCount int32
	DeathCount int32
	Gold      int32
}

func (s *RankingSystem) recalculate() {
	s.recalcHero()
	s.recalcClan()
	s.recalcKill()
	s.recalcWealth()
}

func (s *RankingSystem) recalcHero() {
	var players []rankedPlayer
	s.ws.AllPlayers(func(p *world.PlayerInfo) {
		players = append(players, rankedPlayer{
			Name:      p.Name,
			Level:     p.Level,
			ClassType: p.ClassType,
		})
	})

	sort.Slice(players, func(i, j int) bool {
		return players[i].Level > players[j].Level
	})

	newHeroes := make(map[string]bool)

	// 清空舊排名
	s.heroAll = [RankingMax]handler.RankingEntry{}
	s.heroClass = [8][RankingClassMax]handler.RankingEntry{}

	// 全職業 TOP10
	for i := 0; i < len(players) && i < RankingMax; i++ {
		s.heroAll[i] = handler.RankingEntry{Name: players[i].Name, Value: int64(players[i].Level)}
		newHeroes[players[i].Name] = true
	}

	// 各職業 TOP3
	classCount := [8]int{}
	for _, p := range players {
		ct := int(p.ClassType)
		if ct < 0 || ct >= 8 {
			continue
		}
		if classCount[ct] < RankingClassMax {
			s.heroClass[ct][classCount[ct]] = handler.RankingEntry{Name: p.Name, Value: int64(p.Level)}
			newHeroes[p.Name] = true
			classCount[ct]++
		}
	}

	s.heroNames = newHeroes
}

func (s *RankingSystem) recalcClan() {
	type clanEntry struct {
		Name        string
		OnlineCount int
	}
	var clans []clanEntry

	// 統計每個血盟的線上成員數
	counted := make(map[int32]bool)
	s.ws.AllPlayers(func(p *world.PlayerInfo) {
		if p.ClanID == 0 || counted[p.ClanID] {
			return
		}
		clan := s.ws.Clans.GetClan(p.ClanID)
		if clan == nil {
			return
		}
		counted[p.ClanID] = true

		online := 0
		for charID := range clan.Members {
			if s.ws.GetByCharID(charID) != nil {
				online++
			}
		}
		if online > 0 {
			clans = append(clans, clanEntry{Name: clan.ClanName, OnlineCount: online})
		}
	})

	sort.Slice(clans, func(i, j int) bool {
		return clans[i].OnlineCount > clans[j].OnlineCount
	})

	s.clanRanking = [RankingMax]handler.RankingEntry{}
	for i := 0; i < len(clans) && i < RankingMax; i++ {
		s.clanRanking[i] = handler.RankingEntry{Name: clans[i].Name, Value: int64(clans[i].OnlineCount)}
	}
}

func (s *RankingSystem) recalcKill() {
	var kills, deaths []rankedPlayer
	s.ws.AllPlayers(func(p *world.PlayerInfo) {
		if p.KillCount > 0 {
			kills = append(kills, rankedPlayer{Name: p.Name, KillCount: p.KillCount})
		}
		if p.DeathCount > 0 {
			deaths = append(deaths, rankedPlayer{Name: p.Name, DeathCount: p.DeathCount})
		}
	})

	sort.Slice(kills, func(i, j int) bool { return kills[i].KillCount > kills[j].KillCount })
	sort.Slice(deaths, func(i, j int) bool { return deaths[i].DeathCount > deaths[j].DeathCount })

	s.killRanking = [RankingMax]handler.RankingEntry{}
	for i := 0; i < len(kills) && i < RankingMax; i++ {
		s.killRanking[i] = handler.RankingEntry{Name: kills[i].Name, Value: int64(kills[i].KillCount)}
	}

	s.deathRanking = [RankingMax]handler.RankingEntry{}
	for i := 0; i < len(deaths) && i < RankingMax; i++ {
		s.deathRanking[i] = handler.RankingEntry{Name: deaths[i].Name, Value: int64(deaths[i].DeathCount)}
	}
}

func (s *RankingSystem) recalcWealth() {
	type wealthEntry struct {
		Name string
		Gold int32
	}
	var list []wealthEntry

	s.ws.AllPlayers(func(p *world.PlayerInfo) {
		if p.Inv == nil {
			return
		}
		gold := p.Inv.GetAdena()
		if gold > 0 {
			list = append(list, wealthEntry{Name: p.Name, Gold: gold})
		}
	})

	sort.Slice(list, func(i, j int) bool { return list[i].Gold > list[j].Gold })

	s.wealthRanking = [RankingMax]handler.RankingEntry{}
	for i := 0; i < len(list) && i < RankingMax; i++ {
		s.wealthRanking[i] = handler.RankingEntry{Name: list[i].Name, Value: int64(list[i].Gold)}
	}
}

// trimEntries 移除空的排名項目。
func trimEntries(entries []handler.RankingEntry) []handler.RankingEntry {
	var result []handler.RankingEntry
	for _, e := range entries {
		if e.Name != "" {
			result = append(result, e)
		}
	}
	return result
}
