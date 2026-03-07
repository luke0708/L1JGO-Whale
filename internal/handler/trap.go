package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// handleTrapTrigger 處理玩家踩到陷阱。薄層：委派給 TrapSystem 執行遊戲邏輯。
// Java: WorldTrap.onPlayerMoved() → L1TrapInstance.onTrod() → L1Trap.onTrod()。
func handleTrapTrigger(sess *net.Session, player *world.PlayerInfo, traps []*world.TrapInstance, deps *Deps) {
	if deps.Trap != nil {
		deps.Trap.TriggerTraps(sess, player, traps)
	}
}
