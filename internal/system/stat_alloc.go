package system

import (
	"fmt"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

const (
	maxStatValue int16 = 35 // 每個屬性上限
)

// StatAllocSystem 處理角色屬性配點邏輯。
// 實作 handler.StatAllocManager 介面。
type StatAllocSystem struct {
	deps *handler.Deps
}

// NewStatAllocSystem 建立屬性配點系統。
func NewStatAllocSystem(deps *handler.Deps) *StatAllocSystem {
	return &StatAllocSystem{deps: deps}
}

// AllocStat 分配一個屬性點。回傳是否成功。
func (s *StatAllocSystem) AllocStat(sess *net.Session, player *world.PlayerInfo, statName string) {
	// 檢查可用配點
	available := player.Level - 50 - player.BonusStats
	if available <= 0 {
		return
	}

	// 檢查六屬性總和上限
	totalStats := player.Str + player.Dex + player.Con + player.Wis + player.Intel + player.Cha
	if totalStats >= maxTotalStats {
		return
	}

	// 套用屬性增加
	switch statName {
	case "str":
		if player.Str >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Str++
	case "dex":
		if player.Dex >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Dex++
	case "con":
		if player.Con >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Con++
	case "wis":
		if player.Wis >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Wis++
	case "int":
		if player.Intel >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Intel++
	case "cha":
		if player.Cha >= maxStatValue {
			handler.SendServerMessage(sess, 481)
			return
		}
		player.Cha++
	default:
		return
	}

	player.BonusStats++
	player.Dirty = true

	s.deps.Log.Info(fmt.Sprintf("配點完成  角色=%s  屬性=%s  已用配點=%d", player.Name, statName, player.BonusStats))

	handler.SendPlayerStatus(sess, player)
	handler.SendAbilityScores(sess, player)

	// 若還有剩餘配點，再次顯示配點對話框
	remainingBonus := player.Level - 50 - player.BonusStats
	newTotal := player.Str + player.Dex + player.Con + player.Wis + player.Intel + player.Cha
	if remainingBonus > 0 && newTotal < maxTotalStats {
		handler.SendRaiseAttrDialog(sess, player.CharID)
	}
}
