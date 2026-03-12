package handler

import (
	"fmt"
	"sync/atomic"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// arrowSeqNum is a global sequential number for arrow projectile packets (matches Java AtomicInteger).
var arrowSeqNum atomic.Int32

// showNpcID 控制 NPC 名稱是否附加 NPC ID 和 GFX ID（開發除錯用）。
// 啟動時由 SetShowNpcID 設定，遊戲迴圈單線程讀取，無需原子操作。
var showNpcID bool

// SetShowNpcID 設定 NPC 名稱是否顯示 ID 資訊（啟動時呼叫一次）。
func SetShowNpcID(enabled bool) { showNpcID = enabled }

// sendOwnCharPackPlayer sends S_PUT_OBJECT (opcode 87) for the player's OWN character.
// Uses S_OwnCharPack format (different trailing bytes from S_OtherCharPacks).
// Must be used when sending the character pack to the player themselves (teleport, map change).
// Using S_OtherCharPacks format for own char ID causes the client to misparse → invisible/grey model.
func sendOwnCharPackPlayer(sess *net.Session, p *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(p.X))
	w.WriteH(uint16(p.Y))
	w.WriteD(p.CharID)
	w.WriteH(uint16(PlayerGfx(p)))
	w.WriteC(p.CurrentWeapon)
	w.WriteC(byte(p.Heading))
	w.WriteC(p.LightSize) // light size
	w.WriteC(p.MoveSpeed) // move speed
	w.WriteD(1)           // unknown (always 1)
	w.WriteH(uint16(p.Lawful))
	w.WriteS(p.Name)
	w.WriteS(p.Title)
	status := byte(0x04) // PC flag
	status |= p.BraveSpeed * 16
	w.WriteC(status)
	w.WriteD(0) // clan emblem ID
	w.WriteS(p.ClanName)
	w.WriteS("") // null
	// Clan rank (OwnCharPack specific — OtherCharPacks always writes 0)
	if p.ClanRank > 0 {
		w.WriteC(byte(p.ClanRank << 4))
	} else {
		w.WriteC(0xb0)
	}
	partyHP := byte(0xff)
	if p.PartyID > 0 {
		partyHP = world.CalcPartyHP(p.HP, p.MaxHP)
	}
	w.WriteC(partyHP)
	w.WriteC(0x00) // third speed
	w.WriteC(0x00) // PC = 0
	w.WriteC(0x00) // unknown
	w.WriteC(0xff) // unknown
	w.WriteC(0xff) // unknown
	w.WriteS("")   // null
	w.WriteC(0x00) // end
	sess.Send(w.Bytes())
}

// SendPutObject sends S_PUT_OBJECT (opcode 87) to show another player to the viewer.
// Matches Java S_OtherCharPacks format exactly.
func SendPutObject(viewer *net.Session, p *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(p.X))
	w.WriteH(uint16(p.Y))
	w.WriteD(p.CharID)
	w.WriteH(uint16(PlayerGfx(p))) // use polymorph GFX if active
	w.WriteC(p.CurrentWeapon)    // current weapon visual
	w.WriteC(byte(p.Heading))
	w.WriteC(p.LightSize)        // light size
	w.WriteC(p.MoveSpeed)        // move speed: 0=normal, 1=haste
	w.WriteD(1)                  // unknown (always 1)
	w.WriteH(uint16(p.Lawful))
	w.WriteS(p.Name)
	w.WriteS(p.Title)
	status := byte(0x04)         // bit 2 = PC flag
	status |= p.BraveSpeed * 16  // brave speed encoded in bits 4-5
	w.WriteC(status)             // status flags
	w.WriteD(0)                  // clan emblem ID
	w.WriteS(p.ClanName)
	w.WriteS("")                 // null
	w.WriteC(0)                  // unknown (always 0 for other PCs)
	partyHP := byte(0xff)        // 0xff = not in party
	if p.PartyID > 0 {
		partyHP = world.CalcPartyHP(p.HP, p.MaxHP)
	}
	w.WriteC(partyHP)            // party HP bar (0-10, proportional)
	w.WriteC(0x00)               // third speed
	w.WriteC(0x00)               // PC = 0, NPC = level
	w.WriteS("")                 // private shop / null
	w.WriteC(0xff)               // unknown
	w.WriteC(0xff)               // unknown
	viewer.Send(w.Bytes())
}

// SendRemoveObject sends S_REMOVE_OBJECT (opcode 120) to remove an entity from view.
func SendRemoveObject(viewer *net.Session, charID int32) {
	viewer.Send(BuildRemoveObject(charID))
}

// BuildRemoveObject 建構 S_REMOVE_OBJECT 封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildRemoveObject(charID int32) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_REMOVE_OBJECT)
	w.WriteD(charID)
	return w.Bytes()
}

// sendMoveObject sends S_MOVE_OBJECT (opcode 10) to animate PC movement.
// Sends the PREVIOUS position + heading — client calculates destination.
// Java S_MoveCharPacket constructor 1: [C op][D id][H locX][H locY][C heading][H 0]
func sendMoveObject(viewer *net.Session, charID int32, prevX, prevY int32, heading int16) {
	viewer.Send(BuildMoveObject(charID, prevX, prevY, heading))
}

// BuildMoveObject 建構玩家移動封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildMoveObject(charID int32, prevX, prevY int32, heading int16) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_MOVE_OBJECT)
	w.WriteD(charID)
	w.WriteH(uint16(prevX))
	w.WriteH(uint16(prevY))
	w.WriteC(byte(heading))
	w.WriteH(0)
	return w.Bytes()
}

// sendChangeHeading sends S_CHANGEHEADING (opcode 122) — direction change to nearby players.
// Format: [D objectId][C heading]
func sendChangeHeading(viewer *net.Session, charID int32, heading int16) {
	viewer.Send(BuildChangeHeading(charID, heading))
}

// BuildChangeHeading 建構方向變更封包位元組（不發送）。
func BuildChangeHeading(charID int32, heading int16) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHANGEHEADING)
	w.WriteD(charID)
	w.WriteC(byte(heading))
	return w.Bytes()
}

// SendWeather 匯出 sendWeather — 供 system 套件發送天氣封包。
func SendWeather(sess *net.Session, weather byte) {
	sendWeather(sess, weather)
}

// sendWeather sends S_WEATHER (opcode 115).
func sendWeather(sess *net.Session, weather byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_WEATHER)
	w.WriteC(weather)
	sess.Send(w.Bytes())
}

// sendLight 發送 S_Light (opcode 40) — 角色光源大小。
// Java S_Light: writeC(opcode) + writeD(objID) + writeC(lightSize)
// lightSize: 0=無光, 14=日光術, 最大值=亮光圈半徑
func sendLight(sess *net.Session, objID int32, lightSize byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHANGE_LIGHT)
	w.WriteD(objID)
	w.WriteC(lightSize)
	sess.Send(w.Bytes())
}

// BuildLight 建構 S_Light 封包位元組（廣播用）。
func BuildLight(objID int32, lightSize byte) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHANGE_LIGHT)
	w.WriteD(objID)
	w.WriteC(lightSize)
	return w.Bytes()
}

// CalcPlayerLight 計算玩家光源大小（Java turnOnOffLight 邏輯）。
// 優先級：日光術(14) > 道具光源(未實作) > 0
func CalcPlayerLight(p *world.PlayerInfo) byte {
	// 技能 2（日光術）= lightSize 14
	if p.HasBuff(2) {
		return 14
	}
	// TODO: 道具光源（type2=0, type=2, isLighting 的物品 lightRange）
	return 0
}

// UpdatePlayerLight 重新計算並廣播玩家光源。
func UpdatePlayerLight(p *world.PlayerInfo, ws *world.State) {
	newLight := CalcPlayerLight(p)
	if newLight == p.LightSize {
		return
	}
	p.LightSize = newLight

	// 發送給自己
	sendLight(p.Session, p.CharID, newLight)

	// 廣播給附近玩家
	nearby := ws.GetNearbyPlayers(p.X, p.Y, p.MapID, p.SessionID)
	data := BuildLight(p.CharID, newLight)
	BroadcastToPlayers(nearby, data)
}

// sendGameTime sends S_GameTime (opcode 123) — current game time in seconds.
func sendGameTime(sess *net.Session, gameTimeSec int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_TIME)
	w.WriteD(int32(gameTimeSec))
	sess.Send(w.Bytes())
}

// sendMagicStatus sends S_MAGIC_STATUS (opcode 37) — SP and MR.
func sendMagicStatus(sess *net.Session, sp byte, mr uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_MAGIC_STATUS)
	w.WriteC(sp)
	w.WriteH(mr)
	sess.Send(w.Bytes())
}

// SendNpcPack sends S_PUT_OBJECT (opcode 87) for an NPC to the viewer.
func SendNpcPack(viewer *net.Session, npc *world.NpcInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(npc.X))
	w.WriteH(uint16(npc.Y))
	w.WriteD(npc.ID)
	w.WriteH(uint16(npc.GfxID))
	w.WriteC(0)                   // status (0 = normal)
	w.WriteC(byte(npc.Heading))
	w.WriteC(npc.LightSize)       // light
	w.WriteC(0)                   // move speed
	w.WriteD(npc.Exp)             // experience reward
	w.WriteH(0)                   // lawful

	// 除錯模式：名稱後附加 NpcID:GfxID
	if showNpcID {
		w.WriteS(fmt.Sprintf("%s#%d:%d", npc.NameID, npc.NpcID, npc.GfxID))
	} else {
		w.WriteS(npc.NameID)
	}
	w.WriteS("")                  // title
	w.WriteC(0x00)                // ext status: NO PC flag
	w.WriteD(0)                   // reserved
	w.WriteS("")                  // no clan
	w.WriteS("")                  // no master
	w.WriteC(0x00)                // hidden = 0 (normal)
	w.WriteC(0xFF)                // HP% (0xFF = full for initial)
	w.WriteC(0x00)                // reserved
	w.WriteC(byte(npc.Level))     // level
	w.WriteC(0xFF)                // reserved
	w.WriteC(0xFF)                // reserved
	w.WriteC(0x00)                // reserved
	viewer.Send(w.Bytes())
}

// SendNpcDeadPack 發送 S_PUT_OBJECT（status=8）讓客戶端以屍體姿態顯示死亡 NPC。
// 只發給「之後進入視野」的新玩家（Java onPerceive 邏輯）。
// 已在場玩家靠 S_DoActionGFX(8) 維持屍體互動性。
func SendNpcDeadPack(viewer *net.Session, npc *world.NpcInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(npc.X))
	w.WriteH(uint16(npc.Y))
	w.WriteD(npc.ID)
	w.WriteH(uint16(npc.GfxID))
	w.WriteC(8)                   // status = ACTION_Die（屍體姿態）
	w.WriteC(byte(npc.Heading))
	w.WriteC(npc.LightSize)       // light
	w.WriteC(0)                   // move speed
	w.WriteD(npc.Exp)             // exp（Java: 死亡 NPC 仍發 exp）
	w.WriteH(0)                   // lawful

	if showNpcID {
		w.WriteS(fmt.Sprintf("%s#%d:%d", npc.NameID, npc.NpcID, npc.GfxID))
	} else {
		w.WriteS(npc.NameID)
	}
	w.WriteS("")                  // title
	w.WriteC(0x00)                // ext status
	w.WriteD(0)                   // reserved
	w.WriteS("")                  // no clan
	w.WriteS("")                  // no master
	w.WriteC(0x00)                // object type
	w.WriteC(0xFF)                // HP%（Java: NPC 永遠 0xFF，即使死亡）
	w.WriteC(0x00)                // reserved
	w.WriteC(byte(npc.Level))     // level
	w.WriteC(0xFF)                // reserved
	w.WriteC(0xFF)                // reserved
	w.WriteC(0x00)                // reserved
	viewer.Send(w.Bytes())
}

// sendAttackPacket sends S_ATTACK (opcode 30) — attack animation.
// Format: [C opcode][C actionId][D attackerID][D targetID][H damage][C heading][D 0][C effectFlags]
func sendAttackPacket(viewer *net.Session, attackerID, targetID, damage int32, heading int16) {
	viewer.Send(BuildAttackPacket(attackerID, targetID, damage, heading))
}

// BuildAttackPacket 建構近戰攻擊封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildAttackPacket(attackerID, targetID, damage int32, heading int16) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ATTACK)
	w.WriteC(1)
	w.WriteD(attackerID)
	w.WriteD(targetID)
	w.WriteH(uint16(damage))
	w.WriteC(byte(heading))
	w.WriteD(0)
	w.WriteC(0)
	return w.Bytes()
}

// sendArrowAttackPacket sends S_UseArrowSkill (same opcode 30) — ranged attack with arrow projectile.
// Java: S_UseArrowSkill uses actionId=1 + sequential number + projectile GFX + coordinates.
func sendArrowAttackPacket(viewer *net.Session, attackerID, targetID, damage int32, heading int16, ax, ay, tx, ty int32) {
	seq := arrowSeqNum.Add(1)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ATTACK)
	w.WriteC(1)                    // actionId: 1 = PC attack (same as melee per Java)
	w.WriteD(attackerID)
	w.WriteD(targetID)
	w.WriteH(uint16(damage))
	w.WriteC(byte(heading))
	w.WriteD(seq)                  // sequential number (must be non-zero, incrementing)
	w.WriteH(66)                   // arrowGfxId: 66 = normal arrow projectile
	w.WriteC(0)                    // use_type: 0 = arrow/projectile
	w.WriteH(uint16(ax))          // attacker X
	w.WriteH(uint16(ay))          // attacker Y
	w.WriteH(uint16(tx))          // target X
	w.WriteH(uint16(ty))          // target Y
	w.WriteC(0)                    // effect flags
	w.WriteC(0)
	w.WriteC(0)
	viewer.Send(w.Bytes())
}

// sendUseAttackSkill sends S_UseAttackSkill (opcode 30) — magic attack with projectile.
// Matches Java S_UseAttackSkill format exactly.
// actionId: 18 = ACTION_SkillAttack
// useType: 6 = ranged magic, 8 = ranged AoE magic
func sendUseAttackSkill(viewer *net.Session, casterID, targetID int32, damage int16, heading int16, gfxID int32, useType byte, cx, cy, tx, ty int32) {
	seq := arrowSeqNum.Add(1)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ATTACK)
	w.WriteC(18)                   // actionId: 18 = ACTION_SkillAttack
	w.WriteD(casterID)             // caster char ID (non-zero = show cast motion)
	w.WriteD(targetID)             // target object ID
	w.WriteH(uint16(damage))      // damage
	w.WriteC(byte(heading))        // heading toward target
	w.WriteD(seq)                  // sequential number
	w.WriteH(uint16(gfxID))       // spell GFX ID
	w.WriteC(useType)              // 6=ranged magic, 8=AoE magic
	w.WriteH(uint16(cx))          // caster X
	w.WriteH(uint16(cy))          // caster Y
	w.WriteH(uint16(tx))          // target X
	w.WriteH(uint16(ty))          // target Y
	w.WriteC(0)                    // padding
	w.WriteC(0)                    // padding
	w.WriteC(0)                    // effect flags
	viewer.Send(w.Bytes())
}

// sendHpMeter sends S_HP_METER (opcode 237) — NPC HP bar.
// Format: [C opcode][D objectID][H hpRatio(0-100)]
// 0xFF = 清除 HP 條。
func sendHpMeter(viewer *net.Session, objectID int32, hpRatio int16) {
	viewer.Send(BuildHpMeter(objectID, hpRatio))
}

// BuildHpMeter 建構 NPC HP 條封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildHpMeter(objectID int32, hpRatio int16) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HP_METER)
	w.WriteD(objectID)
	w.WriteH(uint16(hpRatio))
	return w.Bytes()
}

// sendActionGfx sends S_ACTION (opcode 158) — action animation (death, etc.).
// Format: [C opcode][D objectID][C actionCode]
func sendActionGfx(viewer *net.Session, objectID int32, actionCode byte) {
	viewer.Send(BuildActionGfx(objectID, actionCode))
}

// BuildActionGfx 建構動作動畫封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildActionGfx(objectID int32, actionCode byte) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ACTION)
	w.WriteD(objectID)
	w.WriteC(actionCode)
	return w.Bytes()
}

// sendExpUpdate sends S_EXP (opcode 113) — level + cumulative exp.
// Format: [C opcode][C level][D totalExp]
func sendExpUpdate(sess *net.Session, level int16, totalExp int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EXP)
	w.WriteC(byte(level))
	w.WriteD(totalExp)
	sess.Send(w.Bytes())
}

// sendPlayerStatus sends S_STATUS (opcode 8) — full character status update.
// Same format as enterworld sendOwnCharStatus but built from PlayerInfo.
// SendPlayerStatus sends S_STATUS to a player. Exported for system package usage.
func SendPlayerStatus(sess *net.Session, p *world.PlayerInfo) {
	sendPlayerStatus(sess, p)
}

func sendPlayerStatus(sess *net.Session, p *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_STATUS)
	w.WriteD(p.CharID)
	level := p.Level
	if level < 1 {
		level = 1
	} else if level > 127 {
		level = 127
	}
	w.WriteC(byte(level))
	w.WriteD(p.Exp)
	w.WriteC(byte(p.Str))
	w.WriteC(byte(p.Intel))
	w.WriteC(byte(p.Wis))
	w.WriteC(byte(p.Dex))
	w.WriteC(byte(p.Con))
	w.WriteC(byte(p.Cha))
	w.WriteD(p.HP)
	w.WriteD(p.MaxHP)
	w.WriteD(p.MP)
	w.WriteD(p.MaxMP)
	w.WriteC(byte(p.AC))

	gameTime := int32(world.GameTimeNow().Seconds())
	gameTime = gameTime - (gameTime%300)
	w.WriteD(gameTime)

	w.WriteC(byte(p.Food))
	maxW := world.PlayerMaxWeight(p)
	w.WriteC(p.Inv.Weight242(maxW))
	w.WriteH(uint16(p.Lawful))
	w.WriteH(uint16(p.FireRes))
	w.WriteH(uint16(p.WaterRes))
	w.WriteH(uint16(p.WindRes))
	w.WriteH(uint16(p.EarthRes))
	w.WriteD(0) // monster kills (TODO: load from DB)
	sess.Send(w.Bytes())
}

// sendSkillEffect sends S_EFFECT (opcode 55) — GFX effect on an entity.
func sendSkillEffect(viewer *net.Session, objectID int32, gfxID int32) {
	viewer.Send(BuildSkillEffect(objectID, gfxID))
}

// BuildSkillEffect 建構技能特效封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildSkillEffect(objectID int32, gfxID int32) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EFFECT)
	w.WriteD(objectID)
	w.WriteH(uint16(gfxID))
	return w.Bytes()
}

// SendDamageNumbers 發送浮動傷害數字到攻擊者客戶端。
// 利用 S_SkillSoundGFX (opcode 55) 播放數字精靈圖。
// GFX ID 範圍: 個位 12266-12275, 十位 12276-12285, 百位 12286-12295,
// 千位 12296-12305, 萬位 12306-12315, MISS 12316。僅發送給攻擊者本人（非廣播）。
func SendDamageNumbers(sess *net.Session, targetID int32, damage int32) {
	if damage <= 0 {
		// MISS 精靈圖
		sess.Send(BuildSkillEffect(targetID, 12316))
		return
	}
	d := damage
	units := d % 10
	tens := (d / 10) % 10
	hundreds := (d / 100) % 10
	thousands := (d / 1000) % 10
	tenThousands := (d / 10000) % 10

	if units > 0 || tens > 0 || hundreds > 0 || thousands > 0 || tenThousands > 0 {
		sess.Send(BuildSkillEffect(targetID, 12266+units))
	}
	if tens > 0 || hundreds > 0 || thousands > 0 || tenThousands > 0 {
		sess.Send(BuildSkillEffect(targetID, 12276+tens))
	}
	if hundreds > 0 || thousands > 0 || tenThousands > 0 {
		sess.Send(BuildSkillEffect(targetID, 12286+hundreds))
	}
	if thousands > 0 || tenThousands > 0 {
		sess.Send(BuildSkillEffect(targetID, 12296+thousands))
	}
	if tenThousands > 0 {
		sess.Send(BuildSkillEffect(targetID, 12306+tenThousands))
	}
}

// SendDropItem sends S_PUT_OBJECT (opcode 87) for a ground item.
// Same opcode as S_CharPack, but client distinguishes by the status byte (0x00 = item vs 0x04 = PC).
// Matches Java S_DropItem packet format.
func SendDropItem(viewer *net.Session, item *world.GroundItem) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(item.X))
	w.WriteH(uint16(item.Y))
	w.WriteD(item.ID)
	w.WriteH(uint16(item.GrdGfx)) // ground graphic ID
	w.WriteC(0)                    // status
	w.WriteC(0)                    // heading
	w.WriteC(0)                    // light
	w.WriteC(0)                    // speed
	w.WriteD(item.Count)           // item count
	w.WriteH(0)                    // lawful
	w.WriteS(item.Name)            // item display name
	w.WriteS("")                   // title
	w.WriteC(0x00)                 // status flags: 0 = item (not PC)
	w.WriteD(0)                    // reserved
	w.WriteS("")                   // no clan
	w.WriteS("")                   // no master
	w.WriteC(0x00)                 // hidden
	w.WriteC(0xFF)                 // reserved
	w.WriteC(0x00)                 // reserved
	w.WriteC(0x00)                 // level
	w.WriteC(0xFF)                 // reserved
	w.WriteC(0xFF)                 // reserved
	w.WriteC(0x00)                 // reserved
	viewer.Send(w.Bytes())
}

// ==================== Buff Icon Packets ====================

// sendIconShield sends S_SkillIconShield (opcode 216) — AC buff icon.
// Java: [C opcode=216][H time][C type][D 0]
// Types: 2=Shield, 3=ShadowArmor, 6=EarthSkin, 7=EarthBless, 10=IronSkin
// Send time=0 to cancel.
func sendIconShield(sess *net.Session, durationSec uint16, iconType byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SKILLICONSHIELD)
	w.WriteH(durationSec)
	w.WriteC(iconType)
	sess.Send(w.Bytes())
}

// sendIconStrup sends S_Strup (opcode 166) — STR buff icon.
// Java: [C opcode][H time][C currentStr][C weightPercent][C type]
// Types: 2=DressMighty, 5=PhysicalEnchantSTR
// Send time=0 to cancel.
func sendIconStrup(sess *net.Session, durationSec uint16, currentStr byte, iconType byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_STRUP)
	w.WriteH(durationSec)
	w.WriteC(currentStr)
	w.WriteC(0) // weight percent（佔位）
	w.WriteC(iconType)
	sess.Send(w.Bytes())
}

// sendIconDexup sends S_Dexup (opcode 188) — DEX buff icon.
// Java: [C opcode][H time][C currentDex][C type]
// Types: 2=DressDexterity, 5=PhysicalEnchantDEX
// Send time=0 to cancel.
func sendIconDexup(sess *net.Session, durationSec uint16, currentDex byte, iconType byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_DEXUP)
	w.WriteH(durationSec)
	w.WriteC(currentDex)
	w.WriteC(iconType)
	sess.Send(w.Bytes())
}

// sendIconAura sends S_SkillIconAura (opcode 250, sub-opcode 0x16) — aura buff icon.
// Java: [C opcode=250][C 0x16][C iconId][H time]
// iconId uses the Java skill constant (= our skill_id - 1 for aura/elf skills).
// Send time=0 to cancel.
func sendIconAura(sess *net.Session, iconID byte, durationSec uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(0x16) // sub-opcode: aura icon
	w.WriteC(iconID)
	w.WriteH(durationSec)
	sess.Send(w.Bytes())
}

// sendIconGfx sends S_SkillIconGFX (opcode 250) — general buff icon.
// Java: [C opcode=250][C iconId][H time]
// iconId: 34=green potion, 40=Immune to Harm, etc.
// Send time=0 to cancel.
func sendIconGfx(sess *net.Session, iconID byte, durationSec uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(iconID)
	w.WriteH(durationSec)
	sess.Send(w.Bytes())
}

// sendWisdomPotionIcon sends S_PacketBoxWisdomPotion (opcode 250) — 慎重藥水 buff icon。
// Java: S_PacketBoxWisdomPotion: [C opcode=250][C 0x39][C 0x2c][H time]
// Send time=0 to cancel icon.
func sendWisdomPotionIcon(sess *net.Session, timeSec uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(0x39)
	w.WriteC(0x2c)
	w.WriteH(timeSec)
	sess.Send(w.Bytes())
}

// sendBluePotionIcon sends S_SkillIconGFX (opcode 250, icon 34) — blue potion buff icon.
// Java: S_SkillIconGFX(34, time): [C opcode=250][C 34][H time]
// Send time=0 to cancel icon.
func sendBluePotionIcon(sess *net.Session, timeSec uint16) {
	sendIconGfx(sess, 34, timeSec)
}

// SendFoodUpdate 發送食物欄更新。Exported for system package usage.
func SendFoodUpdate(sess *net.Session, food int16) {
	sendFoodUpdate(sess, food)
}

// SendWisdomPotionIcon 發送慎重藥水圖示。Exported for system package usage.
func SendWisdomPotionIcon(sess *net.Session, timeSec uint16) {
	sendWisdomPotionIcon(sess, timeSec)
}

// SendBluePotionIcon 發送藍色藥水圖示。Exported for system package usage.
func SendBluePotionIcon(sess *net.Session, timeSec uint16) {
	sendBluePotionIcon(sess, timeSec)
}

// sendWeightUpdate sends S_PacketBox(WEIGHT) (opcode 250, subcode 10) — lightweight weight bar update.
// Java: S_PacketBox.WEIGHT = 10; format: [C opcode=250][C 10][C weight242]
// Sent after every inventory add/remove/count change.
func sendWeightUpdate(sess *net.Session, p *world.PlayerInfo) {
	maxW := world.PlayerMaxWeight(p)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(10) // subcode: WEIGHT
	w.WriteC(p.Inv.Weight242(maxW))
	sess.Send(w.Bytes())
}

// sendFoodUpdate sends S_PacketBox(FOOD) (opcode 250, subcode 11) — lightweight food bar update.
// Java: S_PacketBox.FOOD = 11; format: [C opcode=250][C 11][C food]
func sendFoodUpdate(sess *net.Session, food int16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(11) // subcode: FOOD
	w.WriteC(byte(food))
	sess.Send(w.Bytes())
}

// sendInvisible sends S_Invis (opcode 171) — invisibility state.
// Java: [C opcode=171][D objectId][C type]
// type: 0=visible, 1=invisible
func sendInvisible(sess *net.Session, objectID int32, invisible bool) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_INVISIBLE)
	w.WriteD(objectID)
	if invisible {
		w.WriteC(1)
	} else {
		w.WriteC(0)
	}
	sess.Send(w.Bytes())
}

// ==================== 狀態異常封包 ====================

// S_Paralysis 子類型常數（Java: S_Paralysis.java）
const (
	ParalysisApply     byte = 0x02 // 麻痺施加
	ParalysisRemove    byte = 0x03 // 麻痺解除
	ParalysisMobApply  byte = 0x04 // 怪物麻痺毒施加
	ParalysisMobRemove byte = 0x05 // 怪物麻痺毒解除
	TeleportLock       byte = 0x06 // 傳送鎖定
	TeleportUnlock     byte = 0x07 // 傳送解鎖（已用於 sendTeleportUnlock）
	SleepApply         byte = 0x0A // 睡眠施加
	SleepRemove        byte = 0x0B // 睡眠解除
	FreezeApply        byte = 0x0C // 凍結施加
	FreezeRemove       byte = 0x0D // 凍結解除
	StunApply          byte = 0x16 // 暈眩施加
	StunRemove         byte = 0x17 // 暈眩解除
	BindApply          byte = 0x18 // 束縛施加
	BindRemove         byte = 0x19 // 束縛解除
)

// sendParalysis 發送 S_Paralysis (opcode 202) 到目標玩家。
// Java 格式：[C opcode=202][C subtype]
// 用於暈眩/凍結/睡眠/麻痺/束縛的施加與解除。
func sendParalysis(sess *net.Session, subtype byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PARALYSIS)
	w.WriteC(subtype)
	sess.Send(w.Bytes())
}

// sendPoison 發送 S_Poison (opcode 165) — 中毒/凍結色調視覺效果。
// Java 格式：[C opcode=165][D objectId][C byte1][C byte2]
// poisonType: 0=治癒（清除色調）, 1=綠色（傷害毒）, 2=灰色（麻痺/凍結）
func sendPoison(viewer *net.Session, objectID int32, poisonType byte) {
	viewer.Send(BuildPoison(objectID, poisonType))
}

// BuildPoison 建構中毒色調封包位元組（不發送）。
// 用於廣播場景：序列化一次、發送多次。
func BuildPoison(objectID int32, poisonType byte) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_POISON)
	w.WriteD(objectID)
	switch poisonType {
	case 1:
		w.WriteC(0x01)
		w.WriteC(0x00)
	case 2:
		w.WriteC(0x00)
		w.WriteC(0x01)
	default:
		w.WriteC(0x00)
		w.WriteC(0x00)
	}
	return w.Bytes()
}

// SendPoison 匯出 sendPoison — 供 system 套件發送中毒色調封包。
func SendPoison(viewer *net.Session, objectID int32, poisonType byte) {
	sendPoison(viewer, objectID, poisonType)
}

// SendWeightUpdate 匯出 sendWeightUpdate — 供 system 套件發送負重更新。
func SendWeightUpdate(sess *net.Session, p *world.PlayerInfo) {
	sendWeightUpdate(sess, p)
}

// SendSkillEffect 匯出 sendSkillEffect — 供 system 套件發送技能特效。
func SendSkillEffect(viewer *net.Session, objectID int32, gfxID int32) {
	sendSkillEffect(viewer, objectID, gfxID)
}

// SendExpUpdate 匯出 sendExpUpdate — 供 system 套件發送經驗值更新。
func SendExpUpdate(sess *net.Session, level int16, totalExp int32) {
	sendExpUpdate(sess, level, totalExp)
}

// SendHpMeter 匯出 sendHpMeter — 供 system 套件發送 HP 條更新。
func SendHpMeter(viewer *net.Session, objectID int32, hpRatio int16) {
	sendHpMeter(viewer, objectID, hpRatio)
}

// SendMagicStatus 匯出 sendMagicStatus — 供 system 套件發送 SP/MR。
func SendMagicStatus(sess *net.Session, sp byte, mr uint16) {
	sendMagicStatus(sess, sp, mr)
}

// sendCurseBlind 發送 S_CurseBlind (opcode 47) — 致盲螢幕遮罩。
// Java 格式：[C opcode=47][H type]
// type: 0=解除, 1=施加, 2=減弱施加
func sendCurseBlind(sess *net.Session, blindType uint16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CURSEBLIND)
	w.WriteH(blindType)
	sess.Send(w.Bytes())
}

// --- Exported wrappers for system package usage ---

// SendActionGfx 廣播施法動畫。
func SendActionGfx(viewer *net.Session, objectID int32, actionCode byte) {
	sendActionGfx(viewer, objectID, actionCode)
}

// SendAttackPacket 廣播近戰攻擊封包。
func SendAttackPacket(viewer *net.Session, attackerID, targetID, damage int32, heading int16) {
	sendAttackPacket(viewer, attackerID, targetID, damage, heading)
}

// SendUseAttackSkill 廣播技能攻擊封包。
func SendUseAttackSkill(viewer *net.Session, casterID, targetID int32, damage int16, heading int16, gfxID int32, useType byte, cx, cy, tx, ty int32) {
	sendUseAttackSkill(viewer, casterID, targetID, damage, heading, gfxID, useType, cx, cy, tx, ty)
}

// SendEffectOnPlayer 發送 S_SkillSoundGFX（opcode 55）特效封包。
func SendEffectOnPlayer(sess *net.Session, charID int32, gfxID int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EFFECT)
	w.WriteD(charID)
	w.WriteH(uint16(gfxID))
	sess.Send(w.Bytes())
}

// SendNpcChatPacket 發送 NPC 對話封包（S_SAY opcode 81）。供 system 套件使用。
func SendNpcChatPacket(sess *net.Session, npcID int32, msg string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SAY)
	w.WriteD(npcID)
	w.WriteC(0x02) // type: NPC say
	w.WriteS(msg)
	sess.Send(w.Bytes())
}

// SendParalysis 發送麻痺/凍結/睡眠狀態封包。
func SendParalysis(sess *net.Session, subtype byte) {
	sendParalysis(sess, subtype)
}

// SendCurseBlind 發送致盲螢幕遮罩封包。
func SendCurseBlind(sess *net.Session, blindType uint16) {
	sendCurseBlind(sess, blindType)
}

// SendIconShield 發送防禦型 buff 圖示。
func SendIconShield(sess *net.Session, durationSec uint16, iconType byte) {
	sendIconShield(sess, durationSec, iconType)
}

// SendIconStrup 發送 STR 增益 buff 圖示。
func SendIconStrup(sess *net.Session, durationSec uint16, currentStr byte, iconType byte) {
	sendIconStrup(sess, durationSec, currentStr, iconType)
}

// SendIconDexup 發送 DEX 增益 buff 圖示。
func SendIconDexup(sess *net.Session, durationSec uint16, currentDex byte, iconType byte) {
	sendIconDexup(sess, durationSec, currentDex, iconType)
}

// SendIconAura 發送光環型 buff 圖示。
func SendIconAura(sess *net.Session, iconID byte, durationSec uint16) {
	sendIconAura(sess, iconID, durationSec)
}

// SendInvisible 發送隱身狀態封包。
func SendInvisible(sess *net.Session, objectID int32, invisible bool) {
	sendInvisible(sess, objectID, invisible)
}

// SendArrowAttackPacket 廣播遠程箭矢攻擊封包。
func SendArrowAttackPacket(viewer *net.Session, attackerID, targetID, damage int32, heading int16, ax, ay, tx, ty int32) {
	sendArrowAttackPacket(viewer, attackerID, targetID, damage, heading, ax, ay, tx, ty)
}

// BuildGreenMessage 建構 S_GreenMessage 封包位元組（不發送）。
// Java: S_GreenMessage — opcode 250, sub 0x54, 0x02, 字串訊息。
// 用於全伺服器公告（擊殺訊息等）。訊息可包含色碼：\f2=黃, \f3=紅。
func BuildGreenMessage(msg string) []byte {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(0x54)
	w.WriteC(0x02)
	w.WriteS(msg)
	return w.Bytes()
}

// SendGreenMessage 發送 S_GreenMessage 到指定 session。
func SendGreenMessage(sess *net.Session, msg string) {
	sess.Send(BuildGreenMessage(msg))
}

// SendPacketBoxHpMsg 發送 S_PacketBoxHpMsg（「你覺得舒服多了」恢復提示）。
// Java: S_PacketBoxHpMsg — opcode 250, sub 31 (MSG_FEEL_GOOD)。
func SendPacketBoxHpMsg(sess *net.Session) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(31) // MSG_FEEL_GOOD
	sess.Send(w.Bytes())
}

// SendWindShackle 發送風之枷鎖 debuff 效果（降低攻擊速度）。
// Java: S_PacketBoxWindShackle — opcode 250, sub 44, [D objID][H time>>2]。
// time 為秒數；time=0 表示移除效果。
func SendWindShackle(sess *net.Session, charID int32, durationSec int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(44) // WIND_SHACKLE
	w.WriteD(charID)
	// Java: time >> 2（將毫秒除以 4）。Go 傳入秒數，需轉換：秒 * 1000 / 4 = 秒 * 250
	t := durationSec * 250
	w.WriteH(uint16(t))
	sess.Send(w.Bytes())
}

// SendDodgeIcon 發送 S_PacketBoxIcon1 閃避率圖示更新。
// Java: S_PacketBoxIcon1 — opcode 250, subcode 0x58（增加）或 0x65（減少）。
// dodge 為當前總閃避值。increase=true 為增加通知，false 為減少通知。
func SendDodgeIcon(sess *net.Session, dodge int16, increase bool) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	if increase {
		w.WriteC(0x58) // dodge up
	} else {
		w.WriteC(0x65) // dodge down
	}
	w.WriteH(uint16(dodge))
	sess.Send(w.Bytes())
}

// BroadcastToPlayers 將預建的封包位元組發送給一組玩家。
// 搭配 BuildXxx 函式使用：序列化一次、發送多次，避免重複建構封包。
func BroadcastToPlayers(viewers []*world.PlayerInfo, data []byte) {
	for _, v := range viewers {
		v.Session.Send(data)
	}
}

// SendGmMessage 發送 GM 訊息到指定 session。
// Java: S_ToGmMessage — 使用 S_OPCODE_NPCSHOUT(161), type=0, npcID=0, \fY 黃色文字前綴。
func SendGmMessage(sess *net.Session, info string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_NPCSHOUT)
	w.WriteC(0) // type=0
	w.WriteD(0) // npcID=0
	w.WriteS("\\fY" + info)
	sess.Send(w.Bytes())
}

// BroadcastToGMs 廣播訊息給所有線上 GM（AccessLevel >= 200）。
// 封包格式同 SendGmMessage，序列化一次、發送多次。
func BroadcastToGMs(ws *world.State, info string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_NPCSHOUT)
	w.WriteC(0) // type=0
	w.WriteD(0) // npcID=0
	w.WriteS("\\fY" + info)
	data := w.Bytes()
	ws.AllPlayers(func(p *world.PlayerInfo) {
		if p.AccessLevel >= 200 {
			p.Session.Send(data)
		}
	})
}
