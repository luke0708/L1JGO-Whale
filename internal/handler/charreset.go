package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// S_CharReset 格式類型（opcode 64 的 sub-type）
const (
	resetFormatInit   byte = 1 // 初始化：顯示職業基礎屬性
	resetFormatLevel  byte = 2 // 升級回應：顯示當前等級/屬性
	resetFormatElixir byte = 3 // 萬能藥覆寫模式
)

// HandleCharReset 處理 C_CharReset (opcode 98)。
// Java: C_CharReset.java — 三階段角色重置（洗點）狀態機。
// Handler 只做解析 + 委派給 CharResetSystem。
func HandleCharReset(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || !player.InCharReset {
		return
	}
	if deps.CharReset == nil {
		return
	}

	stage := r.ReadC()

	switch stage {
	case 0x01: // 初始屬性選擇
		newStr := int16(r.ReadC())
		newInt := int16(r.ReadC())
		newWis := int16(r.ReadC())
		newDex := int16(r.ReadC())
		newCon := int16(r.ReadC())
		newCha := int16(r.ReadC())
		deps.CharReset.ResetStage1(sess, player, newStr, newInt, newWis, newDex, newCon, newCha)

	case 0x02: // 逐級升級
		type2 := r.ReadC()
		if type2 == 0x08 {
			lastAttr := r.ReadC()
			deps.CharReset.ResetStage2Finish(sess, player, lastAttr)
		} else {
			deps.CharReset.ResetStage2(sess, player, type2)
		}

	case 0x03: // 萬能藥覆寫
		newStr := int16(r.ReadC())
		newInt := int16(r.ReadC())
		newWis := int16(r.ReadC())
		newDex := int16(r.ReadC())
		newCon := int16(r.ReadC())
		newCha := int16(r.ReadC())
		deps.CharReset.ResetStage3(sess, player, newStr, newInt, newWis, newDex, newCon, newCha)
	}
}

// StartCharReset 啟動角色重置流程。由 NPC 動作 "ent" 觸發。委派給 CharResetSystem。
func StartCharReset(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if deps.CharReset != nil {
		deps.CharReset.Start(sess, player)
	}
}

// ============================================================
//  S_CharReset 封包建構（純封包序列化，供 system 呼叫）
// ============================================================

// SendCharResetLevel 發送 S_CharReset 格式 2（升級回應）。Exported for system package.
func SendCharResetLevel(sess *net.Session, p *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHARSYNACK)
	w.WriteC(resetFormatLevel) // 格式 2
	w.WriteC(byte(p.ResetTempLevel))
	w.WriteC(byte(p.ResetMaxLevel))
	w.WriteD(p.MaxHP)
	w.WriteD(p.MaxMP)
	w.WriteH(uint16(p.AC))
	w.WriteC(byte(p.Str))
	w.WriteC(byte(p.Intel))
	w.WriteC(byte(p.Wis))
	w.WriteC(byte(p.Dex))
	w.WriteC(byte(p.Con))
	w.WriteC(byte(p.Cha))
	sess.Send(w.Bytes())
}

// SendCharResetInit 發送 S_CharReset 格式 1（初始化：進入重置 UI）。Exported for system package.
func SendCharResetInit(sess *net.Session, initHP, initMP int, maxLevel int16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHARSYNACK)
	w.WriteC(resetFormatInit) // 格式 1
	w.WriteD(int32(initHP))
	w.WriteD(int32(initMP))
	w.WriteC(10) // AC = 10（基礎）
	w.WriteC(byte(maxLevel))
	sess.Send(w.Bytes())
}

// SendCharResetElixir 發送 S_CharReset 格式 3（萬能藥覆寫模式）。Exported for system package.
func SendCharResetElixir(sess *net.Session, point int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CHARSYNACK)
	w.WriteC(resetFormatElixir) // 格式 3
	w.WriteC(byte(point))
	sess.Send(w.Bytes())
}

// SendResetFreeze 發送 S_Paralysis（角色重置凍結/解凍）。Exported for system package.
func SendResetFreeze(sess *net.Session, paraType byte, freeze bool) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PARALYSIS)
	w.WriteC(paraType)
	if freeze {
		w.WriteC(1)
	} else {
		w.WriteC(0)
	}
	sess.Send(w.Bytes())
}

// SendOwnCharPackFromPlayer 使用 PlayerInfo 發送自己角色封包（重置傳送用）。Exported for system package.
func SendOwnCharPackFromPlayer(sess *net.Session, p *world.PlayerInfo) {
	gfx := PlayerGfx(p)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_PUT_OBJECT)
	w.WriteH(uint16(p.X))
	w.WriteH(uint16(p.Y))
	w.WriteD(p.CharID)
	w.WriteH(uint16(gfx))
	w.WriteC(p.CurrentWeapon)
	w.WriteC(byte(p.Heading))
	w.WriteC(p.LightSize) // light
	w.WriteD(0)           // speed
	w.WriteD(0)           // exp
	w.WriteH(uint16(p.Lawful))
	w.WriteS(p.Name)
	w.WriteS(p.Title)
	w.WriteC(0)    // status flags
	w.WriteD(0)    // clanId
	w.WriteS("")   // clanName
	w.WriteS("")   // hpBar
	w.WriteC(0xff) // armor type
	w.WriteC(0)    // pledge status
	w.WriteC(0)    // bonus food
	w.WriteH(0)    // end
	sess.Send(w.Bytes())
}
