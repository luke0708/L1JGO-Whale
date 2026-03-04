package system

import (
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
)

// ScrollTeleportSystem 處理卷軸延遲傳送（Phase 1）。
// 卷軸使用時先發特效，延遲 1 tick 再執行傳送，模擬 Java Thread.sleep(196ms)。
// 3.80C 客戶端對瞬間移動卷軸（40100）有內建特效，其他卷軸需要伺服器端延遲。
type ScrollTeleportSystem struct {
	world *world.State
	deps  *handler.Deps
}

func NewScrollTeleportSystem(ws *world.State, deps *handler.Deps) *ScrollTeleportSystem {
	return &ScrollTeleportSystem{world: ws, deps: deps}
}

func (s *ScrollTeleportSystem) Phase() coresys.Phase { return coresys.PhasePreUpdate }

func (s *ScrollTeleportSystem) Update(_ time.Duration) {
	s.world.AllPlayers(func(p *world.PlayerInfo) {
		if p.ScrollTPTick <= 0 {
			return
		}
		p.ScrollTPTick--
		if p.ScrollTPTick == 0 {
			handler.TeleportPlayer(p.Session, p, p.ScrollTPX, p.ScrollTPY, p.ScrollTPMap, 5, s.deps)
		}
	})
}
