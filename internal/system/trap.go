package system

import (
	"math/rand"
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// ─────────────────────────────────────────────────────────────────────────────
// TrapSystem — 陷阱觸發邏輯（遊戲邏輯層）
// 實作 handler.TrapTriggerer 介面。
// ─────────────────────────────────────────────────────────────────────────────

// TrapSystem 處理陷阱觸發的所有遊戲邏輯：傷害、治療、中毒、技能、傳送。
type TrapSystem struct {
	deps *handler.Deps
}

// NewTrapSystem 建立陷阱系統。
func NewTrapSystem(deps *handler.Deps) *TrapSystem {
	return &TrapSystem{deps: deps}
}

// TriggerTraps 處理玩家踩到陷阱的所有遊戲邏輯。
// Java: WorldTrap.onPlayerMoved() → L1TrapInstance.onTrod() → L1Trap.onTrod()。
func (s *TrapSystem) TriggerTraps(sess *net.Session, player *world.PlayerInfo, traps []*world.TrapInstance) {
	// TODO: GM 不觸發陷阱（Java: isGm() 檢查）— 待 GM 等級系統實作後加入

	for _, trap := range traps {
		if !trap.Alive {
			continue
		}

		tpl := trap.Template

		// 發送動畫效果（Java: L1Trap.sendEffect → S_EffectLocation）
		if tpl.GfxID > 0 {
			s.sendTrapEffect(sess, player, trap)
		}

		// 依類型分派處理
		switch tpl.Type {
		case 1: // 傷害
			s.trapDamage(sess, player, trap)
		case 2: // 治療
			s.trapHeal(sess, player, trap)
		case 3: // 召喚怪物
			// TODO: 需要 NPC spawn 系統支援，暫時記錄日誌
			s.deps.Log.Info("陷阱召喚怪物（尚未實作）",
				zap.Int32("npcID", tpl.MonsterNpcID),
				zap.Int32("count", tpl.MonsterCount),
			)
		case 4: // 中毒
			s.trapPoison(sess, player, trap)
		case 5: // 技能
			s.trapSkill(player, trap)
		case 6: // 傳送
			s.trapTeleport(sess, player, trap)
		}

		// 停用陷阱 + 排入重生佇列
		s.deps.TrapMgr.DisableTrap(trap)

		s.deps.Log.Debug("陷阱觸發",
			zap.String("player", player.Name),
			zap.Int32("trapID", tpl.TrapID),
			zap.Int32("type", tpl.Type),
			zap.Int32("x", trap.X),
			zap.Int32("y", trap.Y),
		)
	}
}

// sendTrapEffect 發送陷阱動畫效果到附近玩家。
// Java: S_EffectLocation — writeC(106) + writeH(x) + writeH(y) + writeH(gfxId)。
func (s *TrapSystem) sendTrapEffect(sess *net.Session, player *world.PlayerInfo, trap *world.TrapInstance) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EFFECTLOCATION)
	w.WriteH(uint16(trap.X))
	w.WriteH(uint16(trap.Y))
	w.WriteH(uint16(trap.Template.GfxID))
	data := w.Bytes()

	// 發送給觸發者
	sess.Send(data)

	// 廣播給附近玩家
	nearby := s.deps.World.GetNearbyPlayers(trap.X, trap.Y, trap.MapID, sess.ID)
	handler.BroadcastToPlayers(nearby, data)
}

// trapDamage 陷阱傷害處理。
// Java: L1Trap.onType1 — dmg = dice.roll(diceCount) + base → receiveDamage()。
func (s *TrapSystem) trapDamage(sess *net.Session, player *world.PlayerInfo, trap *world.TrapInstance) {
	tpl := trap.Template
	if tpl.Base <= 0 {
		return
	}
	dmg := tpl.Base
	for i := int32(0); i < tpl.DiceCount; i++ {
		if tpl.Dice > 0 {
			dmg += rand.Int31n(tpl.Dice) + 1
		}
	}
	player.HP -= dmg
	if player.HP < 0 {
		player.HP = 0
	}
	player.Dirty = true
	handler.SendHpUpdate(sess, player)
}

// trapHeal 陷阱治療處理。
// Java: L1Trap.onType2 — pt = dice.roll(diceCount) + base → healHp()。
func (s *TrapSystem) trapHeal(sess *net.Session, player *world.PlayerInfo, trap *world.TrapInstance) {
	tpl := trap.Template
	if tpl.Base <= 0 {
		return
	}
	heal := tpl.Base
	for i := int32(0); i < tpl.DiceCount; i++ {
		if tpl.Dice > 0 {
			heal += rand.Int31n(tpl.Dice) + 1
		}
	}
	player.HP += heal
	if player.HP > player.MaxHP {
		player.HP = player.MaxHP
	}
	player.Dirty = true
	handler.SendHpUpdate(sess, player)
}

// trapPoison 陷阱中毒處理。
// Java: L1Trap.onType4 — 依 poisonType 施加不同中毒效果。
// TODO: 完整的中毒系統需要 buff/poison 子系統支援。目前僅對一般型中毒造成直接傷害。
func (s *TrapSystem) trapPoison(sess *net.Session, player *world.PlayerInfo, trap *world.TrapInstance) {
	tpl := trap.Template
	switch tpl.PoisonType {
	case 1: // 一般型中毒（直接傷害）
		if tpl.PoisonDamage > 0 {
			player.HP -= int32(tpl.PoisonDamage)
			if player.HP < 0 {
				player.HP = 0
			}
			player.Dirty = true
			handler.SendHpUpdate(sess, player)
		}
	case 2: // 沉默中毒
		s.deps.Log.Debug("陷阱沉默中毒（需 buff 系統）", zap.String("player", player.Name))
	case 3: // 麻痺中毒
		s.deps.Log.Debug("陷阱麻痺中毒（需 buff 系統）", zap.String("player", player.Name))
	}
}

// trapSkill 陷阱技能處理。
// Java: L1Trap.onType5 — 施展指定技能。
func (s *TrapSystem) trapSkill(player *world.PlayerInfo, trap *world.TrapInstance) {
	tpl := trap.Template
	if tpl.SkillID <= 0 {
		return
	}
	// 使用 GM buff 方式施放技能到玩家身上
	if s.deps.Skill != nil {
		s.deps.Skill.ApplyGMBuff(player, tpl.SkillID)
	}
}

// trapTeleport 陷阱傳送處理。
// Java: L1Trap.onType6 — 傳送玩家到指定座標。
func (s *TrapSystem) trapTeleport(sess *net.Session, player *world.PlayerInfo, trap *world.TrapInstance) {
	tpl := trap.Template
	if tpl.TeleportX == 0 && tpl.TeleportY == 0 {
		return
	}
	handler.TeleportPlayer(sess, player, tpl.TeleportX, tpl.TeleportY, int16(tpl.TeleportMapID), 5, s.deps)
}

// ─────────────────────────────────────────────────────────────────────────────
// TrapRespawnSystem — 陷阱重生計時（Phase 3）
// ─────────────────────────────────────────────────────────────────────────────

// TrapRespawnSystem 處理陷阱重生計時。Phase 3（PostUpdate，與 SpawnSystem 同層）。
// Java: ServerTrapTimer — 每 5 秒掃描一次待重生列表。
// Go: 每 tick 檢查到期的重生佇列，由 TrapManager.ProcessRespawns() 處理。
type TrapRespawnSystem struct {
	trapMgr *world.TrapManager
}

// NewTrapRespawnSystem 建立 TrapRespawnSystem。
func NewTrapRespawnSystem(trapMgr *world.TrapManager) *TrapRespawnSystem {
	return &TrapRespawnSystem{trapMgr: trapMgr}
}

// Phase 回傳系統執行階段。
func (s *TrapRespawnSystem) Phase() coresys.Phase { return coresys.PhasePostUpdate }

// Update 每 tick 處理到期的陷阱重生。
func (s *TrapRespawnSystem) Update(_ time.Duration) {
	s.trapMgr.ProcessRespawns(time.Now())
}
