package handler

import (
	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// 技能相關訊息 ID
const (
	msgNotEnoughMP uint16 = 278 // "因魔力不足而無法使用魔法。"
	msgNotEnoughHP uint16 = 279 // "因體力不足而無法使用魔法。"
	msgCastFail    uint16 = 280 // "施展魔法失敗。"
)

// HandleUseSpell processes C_USE_SPELL (opcode 6).
// Thin handler: parse packet → queue to SkillSystem (Phase 2).
// 封包格式依技能不同：
//   一般技能:      [C row][C column][D targetID][H targetX][H targetY]
//   傳送技能(5,69): [C row][C column][H mapID][D bookmarkID]
//   火牆/生命之流:  [C row][C column][H targetX][H targetY]
func HandleUseSpell(sess *net.Session, r *packet.Reader, deps *Deps) {
	row := int32(r.ReadC())
	column := int32(r.ReadC())
	skillID := row*8 + column + 1

	// Java C_UseSkill: 依技能類型讀取不同的後續欄位
	var targetID int32
	if skillID == 5 || skillID == 69 {
		// 指定傳送 / 集體傳送術：[H mapID][D bookmarkID]
		if r.Remaining() >= 2 {
			_ = r.ReadH() // mapID（客戶端發送但伺服器不使用）
		}
		if r.Remaining() >= 4 {
			targetID = r.ReadD() // bookmarkID
		}
	} else {
		// 一般技能：[D targetID][H targetX][H targetY]
		if r.Remaining() >= 4 {
			targetID = r.ReadD()
		}
		if r.Remaining() >= 4 {
			_ = r.ReadH() // targetX
			_ = r.ReadH() // targetY
		}
	}

	if deps.Skill == nil {
		return
	}
	deps.Skill.QueueSkill(SkillRequest{
		SessionID: sess.ID,
		SkillID:   skillID,
		TargetID:  targetID,
	})
}

// ========================================================================
//  薄層轉發 — 委派到 SkillManager（system/skill.go 實作）
// ========================================================================

// cancelAllBuffs 移除所有可取消的 buff。供 handler 內部（如 npcaction.go）呼叫。
func cancelAllBuffs(target *world.PlayerInfo, deps *Deps) {
	if deps.Skill != nil {
		deps.Skill.CancelAllBuffs(target)
	}
}

// TickPlayerBuffs 每 tick 遞減 buff 計時器。供 system/buff_tick.go 呼叫。
func TickPlayerBuffs(p *world.PlayerInfo, deps *Deps) {
	if deps.Skill != nil {
		deps.Skill.TickPlayerBuffs(p)
	}
}

// RemoveBuffAndRevert 移除衝突 buff 並還原屬性。供 system/item_use.go 呼叫。
func RemoveBuffAndRevert(target *world.PlayerInfo, skillID int32, deps *Deps) {
	if deps.Skill != nil {
		deps.Skill.RemoveBuffAndRevert(target, skillID)
	}
}

// ========================================================================
//  Handler 內部共用輔助函式（death.go 等使用）
// ========================================================================

// cancelBuffIcon 取消 buff 圖示（發送 duration=0）。供 death.go 使用。
func cancelBuffIcon(target *world.PlayerInfo, skillID int32, deps *Deps) {
	sendBuffIcon(target, skillID, 0, deps)
}

// sendSpeedToAll 向自己和附近玩家發送速度封包。供 death.go 使用。
func sendSpeedToAll(target *world.PlayerInfo, deps *Deps, speedType byte, duration uint16) {
	sendSpeedPacket(target.Session, target.CharID, speedType, duration)
	nearby := deps.World.GetNearbyPlayers(target.X, target.Y, target.MapID, target.SessionID)
	for _, other := range nearby {
		sendSpeedPacket(other.Session, target.CharID, speedType, 0)
	}
}

// sendBraveToAll 向自己和附近玩家發送勇敢封包。供 death.go 使用。
func sendBraveToAll(target *world.PlayerInfo, deps *Deps, braveType byte, duration uint16) {
	sendBravePacket(target.Session, target.CharID, braveType, duration)
	nearby := deps.World.GetNearbyPlayers(target.X, target.Y, target.MapID, target.SessionID)
	for _, other := range nearby {
		sendBravePacket(other.Session, target.CharID, braveType, 0)
	}
}

// ========================================================================
//  Buff 圖示封包（enterworld.go 使用）
// ========================================================================

// sendBuffIcon sends the appropriate buff icon packet to the client for a given skill.
// Icon mapping is data-driven from buff_icon_map.yaml via deps.BuffIcons.
// Duration in seconds; 0 = cancel.
func sendBuffIcon(target *world.PlayerInfo, skillID int32, durationSec uint16, deps *Deps) {
	icon := deps.BuffIcons.Get(skillID)
	if icon == nil {
		return
	}
	sess := target.Session
	switch icon.Type {
	case "shield":
		sendIconShield(sess, durationSec, icon.Param)
	case "strup":
		sendIconStrup(sess, durationSec, byte(target.Str), icon.Param)
	case "dexup":
		sendIconDexup(sess, durationSec, byte(target.Dex), icon.Param)
	case "aura":
		iconID := byte(skillID - 1)
		if icon.Param > 0 {
			iconID = icon.Param // 自訂 iconID（如破壞盔甲 = 119）
		}
		sendIconAura(sess, iconID, durationSec)
	case "invis":
		sendInvisible(sess, target.CharID, durationSec > 0)
	case "wisdom":
		sendWisdomPotionIcon(sess, durationSec)
	case "blue_potion":
		sendBluePotionIcon(sess, durationSec)
	}
}

// ========================================================================
//  封包建構器
// ========================================================================

// sendSkillList sends S_SkillList (opcode 164) — tells the client which spells the player knows.
// Uses S_SkillList format: [C length=32][32 bytes bitmask][C 0x00 terminator].
func sendSkillList(sess *net.Session, skills []*data.SkillInfo) {
	var skillSlots [32]byte
	for _, sk := range skills {
		idx := sk.SkillLevel - 1
		if idx < 0 || idx >= 32 {
			continue
		}
		skillSlots[idx] |= byte(sk.IDBitmask)
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ADD_SPELL)
	w.WriteC(byte(len(skillSlots)))
	for _, b := range skillSlots {
		w.WriteC(b)
	}
	w.WriteC(0x00)
	sess.Send(w.Bytes())
}

// sendAddSingleSkill sends S_AddSkill (opcode 164) — notifies the client a new spell was learned.
// Uses S_AddSkill format: [C pageSize][28 bytes bitmask][D 0][D 0].
func sendAddSingleSkill(sess *net.Session, skill *data.SkillInfo) {
	var skillSlots [28]byte
	idx := skill.SkillLevel - 1
	if idx < 0 || idx >= 28 {
		return
	}
	skillSlots[idx] = byte(skill.IDBitmask)

	hasLevel5to8 := idx >= 4 && idx <= 7
	hasLevel9to10 := idx >= 8 && idx <= 9

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ADD_SPELL)
	if hasLevel9to10 {
		w.WriteC(100)
	} else if hasLevel5to8 {
		w.WriteC(50)
	} else {
		w.WriteC(32)
	}
	for _, b := range skillSlots {
		w.WriteC(b)
	}
	w.WriteD(0)
	w.WriteD(0)
	sess.Send(w.Bytes())
}

// SendAddSingleSkill 發送新學會的技能封包。Exported for system package usage.
func SendAddSingleSkill(sess *net.Session, skill *data.SkillInfo) {
	sendAddSingleSkill(sess, skill)
}

// ========================================================================
//  工具函式（供 handler 內部其他檔案使用）
// ========================================================================

// chebyshevDist returns the Chebyshev distance between two points.
func chebyshevDist(x1, y1, x2, y2 int32) int32 {
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
