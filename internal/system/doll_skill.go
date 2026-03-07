package system

// doll_skill.go — 魔法娃娃主動技能觸發。
// Java: L1AttackPc.startAttack() 迴圈遍歷 _pc.getDolls()，
// 對每個有 SkillID 的娃娃呼叫 startDollSkill(target, false)。
// 觸發時機：主人近戰/遠程攻擊命中（damage > 0）時。

import (
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/world"
)

// processDollSkillProc 處理娃娃主動技能觸發。
// 主人攻擊命中時呼叫，遍歷所有娃娃，機率觸發技能造成額外傷害 + GFX。
// Java: L1DollInstance.startDollSkill — random.nextInt(100) <= _r
// 回傳額外傷害值（加到主攻擊傷害中）。
func processDollSkillProc(player *world.PlayerInfo, npc *world.NpcInfo, nearby []*world.PlayerInfo, deps *handler.Deps) int32 {
	dolls := deps.World.GetDollsByOwner(player.CharID)
	if len(dolls) == 0 {
		return 0
	}

	var totalDmg int32
	for _, doll := range dolls {
		if doll.SkillID == 0 {
			continue
		}

		// Java: random.nextInt(100) <= _r（包含邊界）
		if world.RandInt(100) > doll.SkillChance {
			continue
		}

		// 從技能表查找 GFX 和傷害值
		skillInfo := deps.Skills.Get(doll.SkillID)
		if skillInfo == nil {
			continue
		}

		// 計算傷害（Java: L1Magic.calcMagicDamage 上限 200）
		dmg := int32(skillInfo.DamageValue)
		if skillInfo.DamageDice > 0 {
			dmg += int32(world.RandInt(skillInfo.DamageDice + 1))
		}
		if dmg > 200 {
			dmg = 200
		}
		if dmg < 1 {
			dmg = 1
		}

		totalDmg += dmg

		// 廣播技能特效（序列化一次，發送多次）
		if skillInfo.CastGfx > 0 {
			gfxData := handler.BuildSkillEffect(npc.ID, skillInfo.CastGfx)
			handler.BroadcastToPlayers(nearby, gfxData)
		}
	}
	return totalDmg
}
