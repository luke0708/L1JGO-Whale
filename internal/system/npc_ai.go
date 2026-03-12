package system

import (
	"math/rand"
	"time"

	coresys "github.com/l1jgo/server/internal/core/system"
	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	gonet "github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/scripting"
	"github.com/l1jgo/server/internal/world"
)

// NpcAISystem processes NPC AI via Lua: Go handles target detection + command
// execution, Lua handles all decision logic. Guard NPCs use a simpler Go-only
// AI path. Phase 2 (Update).
type NpcAISystem struct {
	world *world.State
	deps  *handler.Deps
}

func NewNpcAISystem(ws *world.State, deps *handler.Deps) *NpcAISystem {
	return &NpcAISystem{world: ws, deps: deps}
}

func (s *NpcAISystem) Phase() coresys.Phase { return coresys.PhaseUpdate }

func (s *NpcAISystem) Update(_ time.Duration) {
	for _, npc := range s.world.NpcList() {
		if npc.Dead {
			continue
		}
		// Guard AI: separate branch — simple Go logic, no Lua needed.
		if npc.Impl == "L1Guard" {
			s.tickGuardAI(npc)
			continue
		}
		if npc.Impl == "L1Monster" {
			s.tickMonsterAI(npc)
			continue
		}
		// 非戰鬥 NPC：有 passive_speed 的才隨機行走（鳥、村莊 NPC 等）
		if npc.MoveSpeed > 0 {
			s.tickNpcRandomWalk(npc)
		}
	}
}

// ---------- Non-combat NPC Random Walk ----------

// tickNpcRandomWalk 處理非戰鬥 NPC 的隨機行走（鳥、村莊 NPC 等）。
// Java 參考：L1NpcInstance.noTarget() — 方向 0-7 = 8 方位移動，8-39 = 暫停。
// 距離出生點超過 8 格時，強制走向出生點。
func (s *NpcAISystem) tickNpcRandomWalk(npc *world.NpcInfo) {
	if npc.MoveTimer > 0 {
		npc.MoveTimer--
		return
	}

	// 距離出生點超過 8 格 → 走回家
	if chebyshev32(npc.X, npc.Y, npc.SpawnX, npc.SpawnY) > 8 {
		npcWander(s.world, npc, -2, s.deps.MapData)
		return
	}

	// 還有剩餘步數 → 繼續同方向走
	if npc.WanderDist > 0 {
		npcWander(s.world, npc, -1, s.deps.MapData)
		return
	}

	// 隨機選擇新動作（Java: random 0-39）
	dir := rand.Intn(40)
	if dir < 8 {
		// 0-7: 向該方向移動
		npcWander(s.world, npc, dir, s.deps.MapData)
	} else {
		// 8-39: 暫停不動（機率 80%）
		npc.MoveTimer = calcNpcMoveTicks(npc)
	}
}

// ---------- Monster AI (Lua-driven) ----------

func (s *NpcAISystem) tickMonsterAI(npc *world.NpcInfo) {
	// NPC 法術中毒 tick（每 3 秒扣血）
	tickNpcPoison(npc, s.world, s.deps)

	// 負面狀態：麻痺/暈眩/凍結/睡眠時跳過所有行動
	if npc.Paralyzed || npc.Sleeped {
		tickNpcDebuffs(npc, s.world, s.deps)
		return
	}
	// 即使沒被控也要遞減 debuff 計時器（如致盲等不影響行動的 debuff）
	tickNpcDebuffs(npc, s.world, s.deps)

	// Decrement timers
	if npc.AttackTimer > 0 {
		npc.AttackTimer--
	}
	if npc.MoveTimer > 0 {
		npc.MoveTimer--
	}

	// --- 目標檢測（含仇恨列表回退） ---
	var target *world.PlayerInfo
	if npc.AggroTarget != 0 {
		target = s.world.GetBySession(npc.AggroTarget)
		if target == nil || target.Dead || target.MapID != npc.MapID {
			// 當前目標失效 → 從仇恨列表移除，嘗試回退到次高仇恨
			RemoveHateTarget(npc, npc.AggroTarget)
			npc.AggroTarget = 0
			target = nil
			// 嘗試仇恨列表中的下一個目標
			if nextSID := GetMaxHateTarget(npc); nextSID != 0 {
				if nextTarget := s.world.GetBySession(nextSID); nextTarget != nil &&
					!nextTarget.Dead && nextTarget.MapID == npc.MapID {
					npc.AggroTarget = nextSID
					target = nextTarget
				} else {
					RemoveHateTarget(npc, nextSID)
				}
			}
		}
		// 注意：不在此處檢查安全區域。被動仇恨（被攻擊）不受安全區域限制。
		// 安全區域只阻止主動索敵（agro scan），由下方處理。
		// Java 行為：隱藏之谷等新手區整張地圖都是安全區域，怪物被打一定會反擊。
	}

	// Agro mobs scan for new target if none
	var nearbyPlayers []*world.PlayerInfo
	if target == nil && npc.Agro {
		nearbyPlayers = s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		bestDist := int32(999)
		for _, p := range nearbyPlayers {
			if p.Dead {
				continue
			}
			// Skip players in safety zones (Java: getZoneType() == 1)
			if s.deps.MapData != nil &&
				s.deps.MapData.IsSafetyZone(p.MapID, p.X, p.Y) {
				continue
			}
			dist := chebyshev32(npc.X, npc.Y, p.X, p.Y)
			if dist <= 8 && dist < bestDist {
				bestDist = dist
				target = p
			}
		}
		if target != nil {
			npc.AggroTarget = target.SessionID
			npc.MoveTimer = 0  // snap out of wander — react immediately
			npc.WanderDist = 0
		}
	}

	// 附近無玩家 → 回家 + 跳過 Lua（複用 agro 掃描結果，避免重複 AOI 查詢）
	if target == nil {
		if nearbyPlayers == nil {
			nearbyPlayers = s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		}
		if len(nearbyPlayers) == 0 {
			// 無目標 + 無附近玩家 → 傳送回出生點
			if npc.X != npc.SpawnX || npc.Y != npc.SpawnY {
				s.npcTeleportHome(npc)
			}
			npc.AggroTarget = 0
			ClearHateList(npc)
			return
		}
	}

	// --- Build AIContext for Lua ---
	targetDist := int32(0)
	targetID, targetAC, targetLevel := 0, 0, 0
	targetX, targetY := int32(0), int32(0)
	if target != nil {
		targetDist = chebyshev32(npc.X, npc.Y, target.X, target.Y)
		targetID = int(target.CharID)
		targetAC = int(target.AC)
		targetLevel = int(target.Level)
		targetX = target.X
		targetY = target.Y
	}

	spawnDist := chebyshev32(npc.X, npc.Y, npc.SpawnX, npc.SpawnY)

	// Convert mob skills to Lua entries
	var mobSkills []scripting.MobSkillEntry
	if skills := s.deps.MobSkills.Get(npc.NpcID); skills != nil {
		mobSkills = make([]scripting.MobSkillEntry, len(skills))
		for i, sk := range skills {
			mobSkills[i] = scripting.MobSkillEntry{
				SkillID:       sk.SkillID,
				Type:          sk.Type,
				MpConsume:     sk.MpConsume,
				TriggerRandom: sk.TriggerRandom,
				TriggerHP:     sk.TriggerHP,
				TriggerRange:  sk.TriggerRange,
				ActID:         sk.ActID,
				GfxID:         sk.GfxID,
				Leverage:      sk.Leverage,
				ChangeTarget:  sk.ChangeTarget,
				SummonID:      sk.SummonID,
				SummonMin:     sk.SummonMin,
				SummonMax:     sk.SummonMax,
			}
		}
	}

	ctx := scripting.AIContext{
		NpcID:       int(npc.NpcID),
		X:           int(npc.X),
		Y:           int(npc.Y),
		MapID:       int(npc.MapID),
		HP:          int(npc.HP),
		MaxHP:       int(npc.MaxHP),
		MP:          int(npc.MP),
		MaxMP:       int(npc.MaxMP),
		Level:       int(npc.Level),
		AtkDmg:      int(npc.AtkDmg),
		AtkSpeed:    int(npc.AtkSpeed),
		MoveSpeed:   int(npc.MoveSpeed),
		Ranged:      int(npc.Ranged),
		Agro:        npc.Agro,
		TargetID:    targetID,
		TargetX:     int(targetX),
		TargetY:     int(targetY),
		TargetDist:  int(targetDist),
		TargetAC:    targetAC,
		TargetLevel: targetLevel,
		CanAttack:   npc.AttackTimer <= 0,
		CanMove:     npc.MoveTimer <= 0,
		Skills:      mobSkills,
		WanderDist:  npc.WanderDist,
		SpawnDist:   int(spawnDist),
	}

	// --- Call Lua AI ---
	cmds := s.deps.Scripting.RunNpcAI(ctx)

	// --- Execute commands ---
	for _, cmd := range cmds {
		switch cmd.Type {
		case "attack":
			if target != nil {
				s.npcMeleeAttack(npc, target)
				setNpcAtkCooldown(npc)
			}
		case "ranged_attack":
			if target != nil {
				s.npcRangedAttack(npc, target)
				setNpcAtkCooldown(npc)
			}
		case "skill":
			if cmd.ChangeTarget == 2 {
				// 自我施法（治療/加速等）
				s.executeNpcSelfSkill(npc, cmd.SkillID, cmd.GfxID)
			} else if target != nil {
				s.executeNpcSkill(npc, target, cmd.SkillID, cmd.ActID, cmd.GfxID, cmd.Leverage)
			}
			setNpcAtkCooldown(npc)
		case "summon":
			s.executeNpcSummon(npc, cmd.SummonID, cmd.SummonMin, cmd.SummonMax, cmd.GfxID)
			setNpcAtkCooldown(npc)
		case "flee":
			if target != nil {
				npcFleeFrom(s.world, npc, target.X, target.Y, s.deps.MapData)
				npc.MoveTimer = calcNpcMoveTicks(npc)
			}
		case "move_toward":
			if target != nil {
				npcMoveToward(s.world, npc, target.X, target.Y, s.deps.MapData)
				npc.MoveTimer = calcNpcMoveTicks(npc)
			}
		case "wander":
			npcWander(s.world, npc, cmd.Dir, s.deps.MapData)
		case "lose_aggro":
			npc.AggroTarget = 0
		}
	}
}

// ---------- Guard AI (Go-only) ----------

// tickGuardAI processes a single guard NPC's AI each tick.
// Guards hunt wanted players (isWanted), counter-attack when hit, and return home when idle.
func (s *NpcAISystem) tickGuardAI(npc *world.NpcInfo) {
	// NPC 法術中毒 tick（每 3 秒扣血）
	tickNpcPoison(npc, s.world, s.deps)

	// 負面狀態：麻痺/暈眩/凍結/睡眠時跳過所有行動
	if npc.Paralyzed || npc.Sleeped {
		tickNpcDebuffs(npc, s.world, s.deps)
		return
	}
	tickNpcDebuffs(npc, s.world, s.deps)

	// Decrement timers
	if npc.AttackTimer > 0 {
		npc.AttackTimer--
	}
	if npc.MoveTimer > 0 {
		npc.MoveTimer--
	}

	// --- Target validation ---
	var target *world.PlayerInfo
	if npc.AggroTarget != 0 {
		target = s.world.GetBySession(npc.AggroTarget)
		if target == nil || target.Dead || target.MapID != npc.MapID {
			npc.AggroTarget = 0
			target = nil
		}
		// Lose aggro if target is too far (Java: getTileLineDistance() > 30)
		if target != nil && chebyshev32(npc.X, npc.Y, target.X, target.Y) > 30 {
			npc.AggroTarget = 0
			target = nil
		}
	}

	// --- Target search: scan for wanted players ---
	if target == nil {
		nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		bestDist := int32(999)
		for _, p := range nearby {
			if p.Dead || p.Invisible {
				continue
			}
			// Java L1GuardInstance.searchTarget(): 只追殺通緝犯（isWanted）
			// PinkName（桃紅名＝暫時攻擊他人）不觸發警衛追殺
			if p.WantedTicks <= 0 {
				continue
			}
			dist := chebyshev32(npc.X, npc.Y, p.X, p.Y)
			if dist <= 8 && dist < bestDist {
				bestDist = dist
				target = p
			}
		}
		if target != nil {
			npc.AggroTarget = target.SessionID
			npc.MoveTimer = 0
		}
	}

	// --- Has target: chase and attack ---
	if target != nil {
		dist := chebyshev32(npc.X, npc.Y, target.X, target.Y)
		atkRange := int32(npc.Ranged)
		if atkRange < 1 {
			atkRange = 1
		}

		if dist <= atkRange {
			if npc.AttackTimer <= 0 {
				if npc.Ranged > 1 {
					s.npcRangedAttack(npc, target)
				} else {
					s.npcMeleeAttack(npc, target)
				}
				setNpcAtkCooldown(npc)
			}
		} else {
			if npc.MoveTimer <= 0 {
				npcMoveToward(s.world, npc, target.X, target.Y, s.deps.MapData)
				moveTicks := calcNpcMoveTicks(npc)
				npc.MoveTimer = moveTicks
			}
		}
		return
	}

	// --- No target: return home ---
	if npc.X != npc.SpawnX || npc.Y != npc.SpawnY {
		homeDist := chebyshev32(npc.X, npc.Y, npc.SpawnX, npc.SpawnY)
		if homeDist > 30 {
			s.guardTeleportHome(npc)
			return
		}
		if npc.MoveTimer <= 0 {
			npcMoveToward(s.world, npc, npc.SpawnX, npc.SpawnY, s.deps.MapData)
			moveTicks := calcNpcMoveTicks(npc)
			npc.MoveTimer = moveTicks
		}
	}
}

// guardTeleportHome instantly moves a guard back to its spawn point.
func (s *NpcAISystem) guardTeleportHome(npc *world.NpcInfo) {
	oldX, oldY := npc.X, npc.Y

	// 通知舊位置附近玩家：移除 NPC + 解鎖格子
	oldNearby := s.world.GetNearbyPlayersAt(oldX, oldY, npc.MapID)
	rmData := handler.BuildRemoveObject(npc.ID)
	handler.BroadcastToPlayers(oldNearby, rmData)

	// Update map passability
	if s.deps.MapData != nil {
		s.deps.MapData.SetImpassable(npc.MapID, oldX, oldY, false)
		s.deps.MapData.SetImpassable(npc.SpawnMapID, npc.SpawnX, npc.SpawnY, true)
	}

	// Update position (NPC AOI grid + entity grid)
	s.world.UpdateNpcPosition(npc.ID, npc.SpawnX, npc.SpawnY, 0)
	npc.MapID = npc.SpawnMapID

	// 通知新位置附近玩家：顯示 NPC + 封鎖格子
	newNearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	for _, viewer := range newNearby {
		sendNpcPack(viewer.Session, npc)
	}
}

// npcTeleportHome 將怪物瞬移回出生點（附近無玩家時觸發）。
func (s *NpcAISystem) npcTeleportHome(npc *world.NpcInfo) {
	oldX, oldY := npc.X, npc.Y

	// 通知舊位置附近玩家：移除 NPC + 解鎖格子
	oldNearby := s.world.GetNearbyPlayersAt(oldX, oldY, npc.MapID)
	rmData := handler.BuildRemoveObject(npc.ID)
	handler.BroadcastToPlayers(oldNearby, rmData)

	if s.deps.MapData != nil {
		s.deps.MapData.SetImpassable(npc.MapID, oldX, oldY, false)
		s.deps.MapData.SetImpassable(npc.SpawnMapID, npc.SpawnX, npc.SpawnY, true)
	}

	s.world.UpdateNpcPosition(npc.ID, npc.SpawnX, npc.SpawnY, 0)
	npc.MapID = npc.SpawnMapID

	// 回家時回滿血（Java: NPC 回家 = 重置狀態）
	npc.HP = npc.MaxHP
	npc.MP = npc.MaxMP

	// 通知新位置附近玩家
	newNearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	for _, viewer := range newNearby {
		sendNpcPack(viewer.Session, npc)
	}
}

// ---------- NPC Combat ----------

func (s *NpcAISystem) npcMeleeAttack(npc *world.NpcInfo, target *world.PlayerInfo) {
	// 目標絕對屏障：免疫所有傷害（Java: L1AttackNpc.dmg0）
	if target.AbsoluteBarrier {
		npc.AggroTarget = 0 // NPC 無法攻擊屏障目標，清除仇恨
		return
	}

	// 被攻擊時解除睡眠
	if target.Sleeped {
		target.Sleeped = false
		target.RemoveBuff(62)
		target.RemoveBuff(66)
		target.RemoveBuff(103)
		handler.SendParalysis(target.Session, handler.SleepRemove)
	}

	npc.Heading = calcNpcHeading(npc.X, npc.Y, target.X, target.Y)

	res := s.deps.Scripting.CalcNpcMelee(scripting.CombatContext{
		AttackerLevel:  int(npc.Level),
		AttackerSTR:    int(npc.STR),
		AttackerDEX:    int(npc.DEX),
		AttackerWeapon: int(npc.AtkDmg),
		TargetAC:       int(target.AC),
		TargetLevel:    int(target.Level),
	})

	damage := int32(res.Damage)
	if !res.IsHit || damage < 0 {
		damage = 0
	}

	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)

	// 反擊屏障（skill 91）：近戰攻擊時機率觸發反彈
	// Java 參考: L1AttackNpc.calcDamage() — 檢查 target.hasSkillEffect(COUNTER_BARRIER)
	if damage > 0 && target.HasBuff(91) {
		// 機率判定：probability = probabilityValue(25) ，與 random(1~100) 比較
		prob := 25 // 基礎觸發率
		if world.RandInt(100)+1 <= prob {
			// 計算反彈傷害（Java: calcCounterBarrierDamage — NPC 版本：(STR + Level) << 1）
			cbDmg := int32((int(npc.STR) + int(npc.Level)) << 1)
			// 套用設定倍率（Java: ConfigSkill.COUNTER_BARRIER_DMG = 1.5）
			cbDmg = cbDmg * 3 / 2
			if cbDmg > 0 {
				// 反彈傷害施加在 NPC 身上
				npc.HP -= cbDmg
				if npc.HP < 0 {
					npc.HP = 0
				}
				// 播放反擊屏障觸發特效（GFX 10710）
				handler.BroadcastToPlayers(nearby, handler.BuildSkillEffect(target.CharID, 10710))
				// 原始攻擊傷害歸零
				damage = 0
				// 如果 NPC 被反彈殺死
				if npc.HP <= 0 {
					hpData := handler.BuildHpMeter(npc.ID, 0)
					handler.BroadcastToPlayers(nearby, hpData)
					handleNpcDeath(npc, target, nearby, s.deps)
					npc.AggroTarget = 0
					return
				}
				// 廣播 NPC HP 條
				hpRatio := int16(0)
				if npc.MaxHP > 0 {
					hpRatio = int16((npc.HP * 100) / npc.MaxHP)
				}
				handler.BroadcastToPlayers(nearby, handler.BuildHpMeter(npc.ID, hpRatio))
			}
		}
	}

	atkData := buildNpcAttack(npc.ID, target.CharID, damage, npc.Heading)
	handler.BroadcastToPlayers(nearby, atkData)

	if damage <= 0 {
		return
	}

	target.HP -= int32(damage)
	target.Dirty = true
	if target.HP <= 0 {
		target.HP = 0
		// 守衛擊殺：PK count -1 + 清除通緝（Java L1PcInstance:7393）
		if npc.Impl == "L1Guard" && target.PKCount > 0 {
			target.PKCount--
			target.WantedTicks = 0
		}
		s.deps.Death.KillPlayer(target)
		npc.AggroTarget = 0
		return
	}
	sendHPUpdate(target.Session, target.HP, target.MaxHP)

	// 怪物施毒判定（Java L1AttackNpc.addNpcPoisonAttack）
	if npc.PoisonAtk > 0 {
		ApplyNpcPoisonAttack(npc, target, s.world, s.deps)
	}
}

func (s *NpcAISystem) npcRangedAttack(npc *world.NpcInfo, target *world.PlayerInfo) {
	// 目標絕對屏障：免疫所有傷害
	if target.AbsoluteBarrier {
		npc.AggroTarget = 0
		return
	}

	// 被攻擊時解除睡眠
	if target.Sleeped {
		target.Sleeped = false
		target.RemoveBuff(62)
		target.RemoveBuff(66)
		target.RemoveBuff(103)
		handler.SendParalysis(target.Session, handler.SleepRemove)
	}

	npc.Heading = calcNpcHeading(npc.X, npc.Y, target.X, target.Y)

	res := s.deps.Scripting.CalcNpcRanged(scripting.CombatContext{
		AttackerLevel:  int(npc.Level),
		AttackerSTR:    int(npc.STR),
		AttackerDEX:    int(npc.DEX),
		AttackerWeapon: int(npc.AtkDmg),
		TargetAC:       int(target.AC),
		TargetLevel:    int(target.Level),
	})

	damage := int32(res.Damage)
	if !res.IsHit || damage < 0 {
		damage = 0
	}

	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	rngData := buildNpcRangedAttack(npc.ID, target.CharID, damage, npc.Heading,
		npc.X, npc.Y, target.X, target.Y)
	handler.BroadcastToPlayers(nearby, rngData)

	if damage <= 0 {
		return
	}

	target.HP -= int32(damage)
	target.Dirty = true
	if target.HP <= 0 {
		target.HP = 0
		// 守衛擊殺：PK count -1 + 清除通緝（Java L1PcInstance:7393）
		if npc.Impl == "L1Guard" && target.PKCount > 0 {
			target.PKCount--
			target.WantedTicks = 0
		}
		s.deps.Death.KillPlayer(target)
		npc.AggroTarget = 0
		return
	}
	sendHPUpdate(target.Session, target.HP, target.MaxHP)

	// 怪物施毒判定（Java L1AttackNpc.addNpcPoisonAttack）
	if npc.PoisonAtk > 0 {
		ApplyNpcPoisonAttack(npc, target, s.world, s.deps)
	}
}

// executeNpcSkill handles an NPC using a skill on a player.
// leverage > 0 表示 type 1 物理技能，傷害 = STR * leverage / 10。
func (s *NpcAISystem) executeNpcSkill(npc *world.NpcInfo, target *world.PlayerInfo, skillID, actID, gfxID, leverage int) {
	// 目標絕對屏障：免疫所有傷害和 debuff
	if target.AbsoluteBarrier {
		npc.AggroTarget = 0
		return
	}

	// Type 1 物理技能（leverage > 0）：不需查技能表，直接用 STR * leverage / 10 計算傷害
	// Java 參考：L1MobSkillUse — type == 1 時使用 getStr() * leverage / 10
	if leverage > 0 {
		s.executeNpcPhysicalSkill(npc, target, actID, gfxID, leverage)
		return
	}

	skill := s.deps.Skills.Get(int32(skillID))
	if skill == nil {
		s.npcMeleeAttack(npc, target)
		return
	}

	// Consume MP
	if skill.MpConsume > 0 {
		npc.MP -= int32(skill.MpConsume)
		if npc.MP < 0 {
			npc.MP = 0
		}
	}

	npc.Heading = calcNpcHeading(npc.X, npc.Y, target.X, target.Y)
	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)

	// Spell visual effect: mob-specific gfx_id takes priority, fallback to skill's CastGfx
	gfx := skill.CastGfx
	if gfxID > 0 {
		gfx = int32(gfxID)
	}

	// Determine if this is a magic projectile (has dice/damage) or physical/buff skill
	isMagicProjectile := skill.DamageValue > 0 || skill.DamageDice > 0

	if isMagicProjectile {
		if skill.Area > 0 {
			// AoE 技能：傷害範圍內所有玩家
			useType := byte(8)
			// 先對主目標發送技能動畫
			skillAtkData := buildNpcUseAttackSkill(npc.ID, target.CharID,
				0, npc.Heading, gfx, useType,
				npc.X, npc.Y, target.X, target.Y)
			handler.BroadcastToPlayers(nearby, skillAtkData)

			// 對範圍內每個玩家獨立計算傷害
			area := int32(skill.Area)
			for _, p := range nearby {
				if p.Dead || p.AbsoluteBarrier {
					continue
				}
				if chebyshev32(target.X, target.Y, p.X, p.Y) > area {
					continue
				}
				sctx := scripting.SkillDamageContext{
					SkillID:         int(skill.SkillID),
					DamageValue:     skill.DamageValue,
					DamageDice:      skill.DamageDice,
					DamageDiceCount: skill.DamageDiceCount,
					SkillLevel:      skill.SkillLevel,
					Attr:            skill.Attr,
					AttackerLevel:   int(npc.Level),
					AttackerSTR:     int(npc.STR),
					AttackerDEX:     int(npc.DEX),
					TargetAC:        int(p.AC),
					TargetLevel:     int(p.Level),
					TargetMR:        int(p.MR),
				}
				res := s.deps.Scripting.CalcSkillDamage(sctx)
				dmg := int32(res.Damage)
				if dmg < 1 {
					dmg = 1
				}
				p.HP -= int32(dmg)
				p.Dirty = true
				if p.HP <= 0 {
					p.HP = 0
					s.deps.Death.KillPlayer(p)
					if p.SessionID == npc.AggroTarget {
						npc.AggroTarget = 0
					}
				} else {
					sendHPUpdate(p.Session, p.HP, p.MaxHP)
				}
			}
		} else {
			// 單目標魔法攻擊
			sctx := scripting.SkillDamageContext{
				SkillID:         int(skill.SkillID),
				DamageValue:     skill.DamageValue,
				DamageDice:      skill.DamageDice,
				DamageDiceCount: skill.DamageDiceCount,
				SkillLevel:      skill.SkillLevel,
				Attr:            skill.Attr,
				AttackerLevel:   int(npc.Level),
				AttackerSTR:     int(npc.STR),
				AttackerDEX:     int(npc.DEX),
				TargetAC:        int(target.AC),
				TargetLevel:     int(target.Level),
				TargetMR:        int(target.MR),
			}
			res := s.deps.Scripting.CalcSkillDamage(sctx)
			damage := int32(res.Damage)
			if damage < 1 {
				damage = 1
			}

			skillAtkData := buildNpcUseAttackSkill(npc.ID, target.CharID,
				int16(damage), npc.Heading, gfx, 6,
				npc.X, npc.Y, target.X, target.Y)
			handler.BroadcastToPlayers(nearby, skillAtkData)

			target.HP -= int32(damage)
			target.Dirty = true
			if target.HP <= 0 {
				target.HP = 0
				s.deps.Death.KillPlayer(target)
				npc.AggroTarget = 0
				return
			}
			sendHPUpdate(target.Session, target.HP, target.MaxHP)
		}
	} else {
		// 非傷害技能（debuff）：發送特效 + 套用 debuff 狀態
		if gfx > 0 {
			effData := handler.BuildSkillEffect(target.CharID, gfx)
			handler.BroadcastToPlayers(nearby, effData)
		}
		// 透過 SkillManager 套用 buff/debuff 效果（麻痺、睡眠、減速等）
		if s.deps.Skill != nil {
			s.deps.Skill.ApplyNpcDebuff(target, skill)
		}
	}
}

// executeNpcPhysicalSkill 處理 NPC type 1 物理技能（leverage 倍率傷害）。
// Java 參考：L1MobSkillUse — type == 1 時 damage = STR * leverage / 10，
// 播放 actID 動作動畫（非魔法 GFX），走物理命中判定。
func (s *NpcAISystem) executeNpcPhysicalSkill(npc *world.NpcInfo, target *world.PlayerInfo, actID, gfxID, leverage int) {
	npc.Heading = calcNpcHeading(npc.X, npc.Y, target.X, target.Y)

	// 物理命中判定（使用 Lua 戰鬥公式）
	res := s.deps.Scripting.CalcNpcMelee(scripting.CombatContext{
		AttackerLevel:  int(npc.Level),
		AttackerSTR:    int(npc.STR),
		AttackerDEX:    int(npc.DEX),
		AttackerWeapon: int(npc.AtkDmg),
		TargetAC:       int(target.AC),
		TargetLevel:    int(target.Level),
	})

	if !res.IsHit {
		// miss — 只播動作動畫，傷害 0
		nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		atkData := buildNpcAttack(npc.ID, target.CharID, 0, npc.Heading)
		handler.BroadcastToPlayers(nearby, atkData)
		return
	}

	// 計算 leverage 傷害（Java: STR * leverage / 10）
	damage := int32(npc.STR) * int32(leverage) / 10
	if damage < 1 {
		damage = 1
	}

	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)

	// 播放技能動畫（如有 GFX 特效則同時顯示）
	if gfxID > 0 {
		effData := handler.BuildSkillEffect(npc.ID, int32(gfxID))
		handler.BroadcastToPlayers(nearby, effData)
	}

	// 廣播攻擊封包（物理傷害）
	atkData := buildNpcAttack(npc.ID, target.CharID, damage, npc.Heading)
	handler.BroadcastToPlayers(nearby, atkData)

	// 被攻擊時解除睡眠
	if target.Sleeped {
		target.Sleeped = false
		target.RemoveBuff(62)
		target.RemoveBuff(66)
		target.RemoveBuff(103)
		handler.SendParalysis(target.Session, handler.SleepRemove)
	}

	target.HP -= int32(damage)
	target.Dirty = true
	if target.HP <= 0 {
		target.HP = 0
		s.deps.Death.KillPlayer(target)
		npc.AggroTarget = 0
		return
	}
	sendHPUpdate(target.Session, target.HP, target.MaxHP)
}

// executeNpcSelfSkill 處理 NPC 自我施法（change_target == 2）。
// 主要用途：自我治療（恢復 HP）、自我加速（haste）、自我 buff。
// Java 參考：L1MobSkillUse.changeMobTarget() 中 changeTarget == 2 分支。
func (s *NpcAISystem) executeNpcSelfSkill(npc *world.NpcInfo, skillID, gfxID int) {
	skill := s.deps.Skills.Get(int32(skillID))
	if skill == nil {
		return
	}

	// 消耗 MP
	if skill.MpConsume > 0 {
		npc.MP -= int32(skill.MpConsume)
		if npc.MP < 0 {
			npc.MP = 0
		}
	}

	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)

	// 播放特效
	gfx := skill.CastGfx
	if gfxID > 0 {
		gfx = int32(gfxID)
	}
	if gfx > 0 {
		effData := handler.BuildSkillEffect(npc.ID, gfx)
		handler.BroadcastToPlayers(nearby, effData)
	}

	// 自我治療：有 DamageValue 時作為治療量
	if skill.DamageValue > 0 || skill.DamageDice > 0 {
		heal := int32(skill.DamageValue)
		if skill.DamageDice > 0 {
			heal += int32(rand.Intn(int(skill.DamageDice)) + 1)
		}
		npc.HP += heal
		if npc.HP > npc.MaxHP {
			npc.HP = npc.MaxHP
		}
		// 廣播 NPC HP 條
		hpRatio := int16(0)
		if npc.MaxHP > 0 {
			hpRatio = int16((npc.HP * 100) / npc.MaxHP)
		}
		handler.BroadcastToPlayers(nearby, handler.BuildHpMeter(npc.ID, hpRatio))
	}

	// 自我加速：Haste 系列技能（Java: NPC 使用 speed-up 技能時設定移動倍率）
	// 透過 debuff 機制暫存加速效果（正面效果也用 debuff 計時器管理）
	if skill.BuffDuration > 0 {
		npc.AddDebuff(int32(skillID), int(skill.BuffDuration)*5) // 秒 → ticks
	}
}

// executeNpcSummon 處理 NPC 召喚小怪（type 3 技能）。
// Java 參考：L1MobSkillUseSpawn — 在施法者附近隨機位置生成指定 NPC。
// 被召喚的怪物不掉落物品（Java: _storeDroped = true），但這裡簡化為普通怪物。
func (s *NpcAISystem) executeNpcSummon(npc *world.NpcInfo, summonID int32, summonMin, summonMax, gfxID int) {
	if summonID <= 0 {
		return
	}

	tmpl := s.deps.Npcs.Get(summonID)
	if tmpl == nil {
		return
	}

	// 計算召喚數量
	count := summonMin
	if summonMax > summonMin {
		count = summonMin + rand.Intn(summonMax-summonMin+1)
	}
	if count <= 0 {
		count = 1
	}
	if count > 8 {
		count = 8 // 上限保護
	}

	nearby := s.world.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)

	// 播放召喚特效
	if gfxID > 0 {
		effData := handler.BuildSkillEffect(npc.ID, int32(gfxID))
		handler.BroadcastToPlayers(nearby, effData)
	}

	// 在 NPC 周圍生成小怪
	for i := 0; i < count; i++ {
		// 在 NPC 附近 3 格內隨機找可走位置
		sx, sy := npc.X, npc.Y
		for try := 0; try < 10; try++ {
			tx := npc.X + int32(rand.Intn(7)) - 3
			ty := npc.Y + int32(rand.Intn(7)) - 3
			if s.deps.MapData != nil && s.deps.MapData.IsPassablePoint(npc.MapID, tx, ty) {
				if !s.world.IsOccupied(tx, ty, npc.MapID, 0) {
					sx, sy = tx, ty
					break
				}
			}
		}

		// 解析速度（與 main.go 相同邏輯）
		atkSpeed := tmpl.AtkSpeed
		moveSpeed := tmpl.PassiveSpeed
		if s.deps.SprTable != nil {
			gfx := int(tmpl.GfxID)
			if tmpl.AtkSpeed != 0 {
				if v := s.deps.SprTable.GetAttackSpeed(gfx, data.ActAttack); v > 0 {
					atkSpeed = int16(v)
				}
			}
			if tmpl.PassiveSpeed != 0 {
				if v := s.deps.SprTable.GetMoveSpeed(gfx, data.ActWalk); v > 0 {
					moveSpeed = int16(v)
				}
			}
		}

		summonNpc := &world.NpcInfo{
			ID:           world.NextNpcID(),
			NpcID:        tmpl.NpcID,
			Impl:         tmpl.Impl,
			GfxID:        tmpl.GfxID,
			Name:         tmpl.Name,
			NameID:       tmpl.NameID,
			Level:        tmpl.Level,
			X:            sx,
			Y:            sy,
			MapID:        npc.MapID,
			HP:           tmpl.HP,
			MaxHP:        tmpl.HP,
			MP:           tmpl.MP,
			MaxMP:        tmpl.MP,
			AC:           tmpl.AC,
			STR:          tmpl.STR,
			DEX:          tmpl.DEX,
			Exp:          tmpl.Exp,
			Lawful:       tmpl.Lawful,
			Size:         tmpl.Size,
			MR:           tmpl.MR,
			Undead:       tmpl.Undead,
			Agro:         tmpl.Agro,
			AtkDmg:       int32(tmpl.Level) + int32(tmpl.STR)/3,
			Ranged:       tmpl.Ranged,
			AtkSpeed:     atkSpeed,
			MoveSpeed:    moveSpeed,
			PoisonAtk:    tmpl.PoisonAtk,
			FireRes:      tmpl.FireRes,
			WaterRes:     tmpl.WaterRes,
			WindRes:      tmpl.WindRes,
			EarthRes:     tmpl.EarthRes,
			SpawnX:       sx,
			SpawnY:       sy,
			SpawnMapID:   npc.MapID,
			RespawnDelay: 0, // 召喚怪物不重生
		}

		s.world.AddNpc(summonNpc)
		if s.deps.MapData != nil {
			s.deps.MapData.SetImpassable(npc.MapID, sx, sy, true)
		}

		// 通知附近玩家顯示新 NPC
		for _, viewer := range nearby {
			sendNpcPack(viewer.Session, summonNpc)
		}
	}
}

// ---------- NPC Movement ----------

// npcMoveToward moves NPC 1 tile toward a target position.
// If the direct path is blocked, tries two alternate side-step directions.
func npcMoveToward(ws *world.State, npc *world.NpcInfo, tx, ty int32, maps *data.MapDataTable) {
	dx := tx - npc.X
	dy := ty - npc.Y

	type candidate struct{ x, y int32 }
	candidates := make([]candidate, 0, 3)

	// Primary: direct toward target
	mx, my := npc.X, npc.Y
	if dx > 0 {
		mx++
	} else if dx < 0 {
		mx--
	}
	if dy > 0 {
		my++
	} else if dy < 0 {
		my--
	}
	candidates = append(candidates, candidate{mx, my})

	// Side-steps
	if dx != 0 && dy != 0 {
		candidates = append(candidates, candidate{mx, npc.Y})
		candidates = append(candidates, candidate{npc.X, my})
	} else if dx != 0 {
		candidates = append(candidates, candidate{mx, npc.Y + 1})
		candidates = append(candidates, candidate{mx, npc.Y - 1})
	} else if dy != 0 {
		candidates = append(candidates, candidate{npc.X + 1, my})
		candidates = append(candidates, candidate{npc.X - 1, my})
	}

	for _, c := range candidates {
		if c.x == npc.X && c.y == npc.Y {
			continue
		}
		h := calcNpcHeading(npc.X, npc.Y, c.x, c.y)

		if maps != nil && !maps.IsPassable(npc.MapID, npc.X, npc.Y, int(h)) {
			continue
		}
		occupant := ws.OccupantAt(c.x, c.y, npc.MapID)
		if occupant > 0 && occupant < 200_000_000 {
			continue
		}

		npcExecuteMove(ws, npc, c.x, c.y, h, maps)
		return
	}
	// All candidates blocked — last resort: pass through
	h := calcNpcHeading(npc.X, npc.Y, mx, my)
	if maps == nil || maps.IsPassableIgnoreOccupant(npc.MapID, npc.X, npc.Y, int(h)) {
		npcExecuteMove(ws, npc, mx, my, h, maps)
	}
}

// npcExecuteMove performs the actual NPC position update and broadcasts.
func npcExecuteMove(ws *world.State, npc *world.NpcInfo, moveX, moveY int32, heading int16, maps *data.MapDataTable) {
	oldX, oldY := npc.X, npc.Y

	if maps != nil {
		maps.SetImpassable(npc.MapID, oldX, oldY, false)
		maps.SetImpassable(npc.MapID, moveX, moveY, true)
	}

	ws.UpdateNpcPosition(npc.ID, moveX, moveY, heading)

	nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	data := buildNpcMove(npc.ID, oldX, oldY, npc.Heading)
	handler.BroadcastToPlayers(nearby, data)
}

// npcFleeFrom 讓 NPC 遠離目標移動 1 格。
// Java 參考：L1NpcInstance.setHomeDir() — 計算反向方向。
func npcFleeFrom(ws *world.State, npc *world.NpcInfo, tx, ty int32, maps *data.MapDataTable) {
	// 計算反向（從目標 → 自己的方向 = 遠離方向）
	dx := npc.X - tx
	dy := npc.Y - ty

	type candidate struct{ x, y int32 }
	candidates := make([]candidate, 0, 3)

	// 主要方向：直接遠離目標
	mx, my := npc.X, npc.Y
	if dx > 0 {
		mx++
	} else if dx < 0 {
		mx--
	}
	if dy > 0 {
		my++
	} else if dy < 0 {
		my--
	}
	candidates = append(candidates, candidate{mx, my})

	// 側向備選
	if dx != 0 && dy != 0 {
		candidates = append(candidates, candidate{mx, npc.Y})
		candidates = append(candidates, candidate{npc.X, my})
	} else if dx != 0 {
		candidates = append(candidates, candidate{mx, npc.Y + 1})
		candidates = append(candidates, candidate{mx, npc.Y - 1})
	} else if dy != 0 {
		candidates = append(candidates, candidate{npc.X + 1, my})
		candidates = append(candidates, candidate{npc.X - 1, my})
	}

	for _, c := range candidates {
		if c.x == npc.X && c.y == npc.Y {
			continue
		}
		h := calcNpcHeading(npc.X, npc.Y, c.x, c.y)
		if maps != nil && !maps.IsPassable(npc.MapID, npc.X, npc.Y, int(h)) {
			continue
		}
		occupant := ws.OccupantAt(c.x, c.y, npc.MapID)
		if occupant > 0 && occupant < 200_000_000 {
			continue
		}
		npcExecuteMove(ws, npc, c.x, c.y, h, maps)
		return
	}
}

// npcWander handles idle wandering. dir: 0-7=new direction, -1=continue, -2=toward spawn.
func npcWander(ws *world.State, npc *world.NpcInfo, dir int, maps *data.MapDataTable) {
	wanderTicks := calcNpcMoveTicks(npc)

	if dir == -1 {
		// Continue current direction
	} else if dir == -2 {
		npc.WanderDir = calcNpcHeading(npc.X, npc.Y, npc.SpawnX, npc.SpawnY)
		npc.WanderDist = rand.Intn(5) + 2
	} else {
		npc.WanderDir = int16(dir)
		npc.WanderDist = rand.Intn(5) + 2
	}

	if npc.WanderDist <= 0 {
		return
	}

	if maps != nil && !maps.IsPassable(npc.MapID, npc.X, npc.Y, int(npc.WanderDir)) {
		npc.WanderDist = 0
		return
	}

	moveX := npc.X + npcHeadingDX[npc.WanderDir]
	moveY := npc.Y + npcHeadingDY[npc.WanderDir]
	npc.WanderDist--
	npc.MoveTimer = wanderTicks

	oldX, oldY := npc.X, npc.Y

	if maps != nil {
		maps.SetImpassable(npc.MapID, oldX, oldY, false)
		maps.SetImpassable(npc.MapID, moveX, moveY, true)
	}

	ws.UpdateNpcPosition(npc.ID, moveX, moveY, npc.WanderDir)

	nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	data := buildNpcMove(npc.ID, oldX, oldY, npc.Heading)
	handler.BroadcastToPlayers(nearby, data)
}

// ---------- Shared utilities ----------

func setNpcAtkCooldown(npc *world.NpcInfo) {
	atkCooldown := 10
	if npc.AtkSpeed > 0 {
		atkCooldown = int(npc.AtkSpeed) / 200
		if atkCooldown < 3 {
			atkCooldown = 3
		}
	}
	npc.AttackTimer = atkCooldown
}

func chebyshev32(x1, y1, x2, y2 int32) int32 {
	dx := x1 - x2
	dy := y1 - y2
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dy > dx {
		return dy
	}
	return dx
}

var npcHeadingDX = [8]int32{0, 1, 1, 1, 0, -1, -1, -1}
var npcHeadingDY = [8]int32{-1, -1, 0, 1, 1, 1, 0, -1}

func calcNpcHeading(sx, sy, tx, ty int32) int16 {
	ddx := tx - sx
	ddy := ty - sy
	if ddx > 0 {
		ddx = 1
	} else if ddx < 0 {
		ddx = -1
	}
	if ddy > 0 {
		ddy = 1
	} else if ddy < 0 {
		ddy = -1
	}
	for i := int16(0); i < 8; i++ {
		if npcHeadingDX[i] == ddx && npcHeadingDY[i] == ddy {
			return i
		}
	}
	return 0
}

// ---------- Packet helpers ----------
// These are local to the system package to avoid circular imports.

// npcArrowSeqNum is a sequential counter for NPC ranged attack packets.
var npcArrowSeqNum int32

// buildNpcMove 建構 NPC 移動封包位元組（不發送）。
// Java S_MoveNpcPacket: [C op][D id][H locX][H locY][C heading][C 0x80]
// 與玩家版不同：NPC 版尾部有 0x80 旗標。
func buildNpcMove(npcID int32, prevX, prevY int32, heading int16) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_MOVE_OBJECT)
	w.WriteD(npcID)
	w.WriteH(uint16(prevX))
	w.WriteH(uint16(prevY))
	w.WriteC(byte(heading))
	w.WriteC(0x80) // NPC 移動旗標（Java S_MoveNpcPacket 第 30 行）
	return w.Bytes()
}

// buildNpcAttack 建構 NPC 近戰攻擊封包位元組（不發送）。
func buildNpcAttack(attackerID, targetID, damage int32, heading int16) []byte {
	return handler.BuildAttackPacket(attackerID, targetID, damage, heading)
}

// buildNpcRangedAttack 建構 NPC 遠程攻擊封包位元組（不發送）。
func buildNpcRangedAttack(attackerID, targetID, damage int32, heading int16, ax, ay, tx, ty int32) []byte {
	npcArrowSeqNum++
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ATTACK)
	w.WriteC(1)
	w.WriteD(attackerID)
	w.WriteD(targetID)
	w.WriteH(uint16(damage))
	w.WriteC(byte(heading))
	w.WriteD(npcArrowSeqNum)
	w.WriteH(66)
	w.WriteC(0)
	w.WriteH(uint16(ax))
	w.WriteH(uint16(ay))
	w.WriteH(uint16(tx))
	w.WriteH(uint16(ty))
	w.WriteC(0)
	w.WriteC(0)
	w.WriteC(0)
	return w.Bytes()
}

func sendNpcPack(sess *gonet.Session, npc *world.NpcInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(npc.X))
	w.WriteH(uint16(npc.Y))
	w.WriteD(npc.ID)
	w.WriteH(uint16(npc.GfxID))
	w.WriteC(0)
	w.WriteC(byte(npc.Heading))
	w.WriteC(0)
	w.WriteC(0)
	w.WriteD(npc.Exp)
	w.WriteH(0)
	w.WriteS(npc.NameID)
	w.WriteS("")
	w.WriteC(0x00)
	w.WriteD(0)
	w.WriteS("")
	w.WriteS("")
	w.WriteC(0x00)
	w.WriteC(0xFF)
	w.WriteC(0x00)
	w.WriteC(byte(npc.Level))
	w.WriteC(0xFF)
	w.WriteC(0xFF)
	w.WriteC(0x00)
	sess.Send(w.Bytes())
}

// buildNpcUseAttackSkill 建構 NPC 技能攻擊封包位元組（不發送）。
func buildNpcUseAttackSkill(casterID, targetID int32, damage int16, heading int16, gfxID int32, useType byte, cx, cy, tx, ty int32) []byte {
	npcArrowSeqNum++
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ATTACK)
	w.WriteC(18)
	w.WriteD(casterID)
	w.WriteD(targetID)
	w.WriteH(uint16(damage))
	w.WriteC(byte(heading))
	w.WriteD(npcArrowSeqNum)
	w.WriteH(uint16(gfxID))
	w.WriteC(useType)
	w.WriteH(uint16(cx))
	w.WriteH(uint16(cy))
	w.WriteH(uint16(tx))
	w.WriteH(uint16(ty))
	w.WriteC(0)
	w.WriteC(0)
	w.WriteC(0)
	return w.Bytes()
}

func sendHPUpdate(sess *gonet.Session, hp, maxHP int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HIT_POINT)
	w.WriteD(hp)
	w.WriteD(maxHP)
	sess.Send(w.Bytes())
}

// ---------- NPC Debuff 計時 ----------

// tickNpcDebuffs 遞減 NPC 的所有 debuff 計時器。到期時清除狀態並廣播解除封包。
func tickNpcDebuffs(npc *world.NpcInfo, ws *world.State, deps *handler.Deps) {
	if len(npc.ActiveDebuffs) == 0 {
		return
	}
	refreshGrey := false // 凍結類 debuff 是否需要定期重發灰色色調
	for skillID, ticksLeft := range npc.ActiveDebuffs {
		ticksLeft--
		if ticksLeft <= 0 {
			delete(npc.ActiveDebuffs, skillID)
			removeNpcDebuffEffect(npc, skillID, ws)
		} else {
			npc.ActiveDebuffs[skillID] = ticksLeft
			// 3.80C 客戶端的 S_Poison 灰色色調會自動淡出，需定期重發維持視覺
			if !refreshGrey && isFreezeDebuff(skillID) && ticksLeft%25 == 0 {
				refreshGrey = true
			}
		}
	}
	if refreshGrey && npc.Paralyzed {
		nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		handler.BroadcastToPlayers(nearby, handler.BuildPoison(npc.ID, 2))
	}
}

// isFreezeDebuff 判斷是否為凍結類 debuff（需要維持灰色色調的技能）。
func isFreezeDebuff(skillID int32) bool {
	switch skillID {
	case 22, 30, 50, 80, 157:
		return true
	}
	return false
}

// removeNpcDebuffEffect 清除 NPC 的 debuff 狀態旗標，並廣播視覺解除封包。
func removeNpcDebuffEffect(npc *world.NpcInfo, skillID int32, ws *world.State) {
	nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
	clearPoison := handler.BuildPoison(npc.ID, 0) // 預建清除色調封包

	switch skillID {
	case 87: // 衝擊之暈 — 解除暈眩
		npc.Paralyzed = false
	case 157: // 大地屏障 — 解除凍結 + 灰色色調
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 33: // 木乃伊詛咒 階段一到期 → 進入階段二（真正麻痺 4 秒）
		npc.Paralyzed = true
		npc.AddDebuff(4001, 20) // 4 秒 = 20 ticks
	case 4001: // 木乃伊詛咒 階段二到期 — 解除麻痺
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 62, 66: // 沉睡之霧 — 解除睡眠
		npc.Sleeped = false
	case 103: // 暗黑盲咒 — 解除睡眠（Java 用 skill 66 的效果）
		npc.Sleeped = false
	case 20, 40: // 闇盲咒術 — 致盲（NPC 無視覺，僅計時）
		// NPC 致盲不影響行動旗標
	case 29, 76, 152: // 緩速系列 — NPC debuff 到期
		// 速度恢復由 calcNpcMoveTicks 自動處理（不再有 slow debuff → 不翻倍）
	case 11: // 毒咒 — 清除傷害毒
		npc.PoisonDmgAmt = 0
		npc.PoisonDmgTimer = 0
		if npc.Paralyzed {
			// NPC 仍在凍結中 → 清除綠色後重發灰色色調
			handler.BroadcastToPlayers(nearby, handler.BuildPoison(npc.ID, 2))
		} else {
			handler.BroadcastToPlayers(nearby, clearPoison)
		}
	case 80: // 冰雪颶風 — 解除凍結
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 22: // 寒冰氣息 — 解除凍結 + 灰色色調
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 30: // 岩牢 — 解除凍結 + 灰色色調
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 50: // 冰矛 — 解除凍結 + 灰色色調
		npc.Paralyzed = false
		handler.BroadcastToPlayers(nearby, clearPoison)
	case 47: // 弱化術 — 僅計時自動清除
	case 56: // 疾病術 — 僅計時自動清除
	}
}

// calcNpcMoveTicks 計算 NPC 移動間隔 tick 數。
// 緩速 debuff（29/76/152）時移動間隔翻倍。
func calcNpcMoveTicks(npc *world.NpcInfo) int {
	moveTicks := 4
	if npc.MoveSpeed > 0 {
		moveTicks = int(npc.MoveSpeed) / 200
		if moveTicks < 2 {
			moveTicks = 2
		}
	}
	// 緩速術效果：移動間隔翻倍（Java: moveSpeed 設為 2 = slow）
	if npc.HasDebuff(29) || npc.HasDebuff(76) || npc.HasDebuff(152) {
		moveTicks *= 2
	}
	return moveTicks
}

// tickNpcPoison 處理 NPC 的法術中毒效果（Java L1DamagePoison 對 NPC）。
// 每 15 tick（3 秒）造成 PoisonDmgAmt 傷害。毒傷害不會殺死 NPC（HP 最低 1）。
func tickNpcPoison(npc *world.NpcInfo, ws *world.State, deps *handler.Deps) {
	if npc.PoisonDmgAmt <= 0 || npc.Dead {
		return
	}

	// 計時（與 debuff 11 綁定）
	if !npc.HasDebuff(11) {
		// debuff 到期 → 清除中毒
		npc.PoisonDmgAmt = 0
		npc.PoisonDmgTimer = 0
		npc.PoisonAttackerSID = 0
		nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		if npc.Paralyzed {
			// NPC 仍在凍結中 → 維持灰色色調
			handler.BroadcastToPlayers(nearby, handler.BuildPoison(npc.ID, 2))
		} else {
			handler.BroadcastToPlayers(nearby, handler.BuildPoison(npc.ID, 0))
		}
		return
	}

	// 仇恨歸屬：毒傷害累加仇恨（Java: NPC 會追擊施毒者）
	if npc.PoisonAttackerSID != 0 {
		AddHate(npc, npc.PoisonAttackerSID, npc.PoisonDmgAmt)
	}

	npc.PoisonDmgTimer++
	if npc.PoisonDmgTimer >= 15 {
		npc.PoisonDmgTimer = 0
		npc.HP -= npc.PoisonDmgAmt
		// 毒傷害不可殺死 NPC — HP 最低 1
		if npc.HP <= 1 {
			npc.HP = 1
		}
		// 廣播 HP 條給所有附近玩家
		nearby := ws.GetNearbyPlayersAt(npc.X, npc.Y, npc.MapID)
		hpRatio := int16(0)
		if npc.MaxHP > 0 {
			hpRatio = int16((npc.HP * 100) / npc.MaxHP)
		}
		handler.BroadcastToPlayers(nearby, handler.BuildHpMeter(npc.ID, hpRatio))
	}
}
