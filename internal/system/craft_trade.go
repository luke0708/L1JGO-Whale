package system

import (
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
)

// CraftTradeSystem 處理製作交易視窗的延遲物品發送（Phase 1）。
// 3.80C 客戶端在同一 tick 收到 S_Trade + S_TradeAddItem 時，交易視窗尚未初始化完成，
// 導致物品不顯示。此系統在 S_Trade 發送 1 tick 後再發送 S_TradeAddItem。
type CraftTradeSystem struct {
	world *world.State
	deps  *handler.Deps
}

func NewCraftTradeSystem(ws *world.State, deps *handler.Deps) *CraftTradeSystem {
	return &CraftTradeSystem{world: ws, deps: deps}
}

func (s *CraftTradeSystem) Phase() coresys.Phase { return coresys.PhasePreUpdate }

func (s *CraftTradeSystem) Update(_ time.Duration) {
	s.world.AllPlayers(func(p *world.PlayerInfo) {
		if p.CraftTradeTick <= 0 {
			return
		}
		p.CraftTradeTick--
		if p.CraftTradeTick == 0 {
			handler.SendCraftTradeItems(p.Session, p, s.deps)
		}
	})
}
