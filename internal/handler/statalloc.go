package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

const (
	statAllocAttrCode uint16 = 479 // Java C_Attr case 479 — stat allocation
	bonusStatMinLevel int16  = 51  // minimum level to earn bonus stat points
	maxTotalStats     int16  = 210 // 六屬性總和上限（enterworld.go 也使用）
)

// HandlePlate processes C_PLATE (opcode 10) — stat point allocation (bonus stats at level 51+).
// NOTE: Opcode 10 is shared with bulletin board (C_Board). HandleBoardOrPlate in board.go
// dispatches to this function when the packet is not a board request.
// Java equivalent: C_Attr case 479.
// Format: [H attrcode(479)][C confirm(1=yes)][S statName]
func HandlePlate(sess *net.Session, r *packet.Reader, deps *Deps) {
	attrCode := r.ReadH()
	confirm := r.ReadC()
	handleStatAlloc(sess, attrCode, confirm, r, deps)
}

// handleStatAlloc is the core stat allocation logic, called either from HandlePlate
// or from HandleBoardOrPlate when opcode 10 is not a board request.
// Handler 只做解析 + 驗證 + 委派給 StatAllocSystem。
func handleStatAlloc(sess *net.Session, attrCode uint16, confirm byte, r *packet.Reader, deps *Deps) {
	if attrCode != statAllocAttrCode {
		return
	}
	if confirm != 1 {
		return
	}

	statName := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Dead {
		return
	}
	if player.Level < bonusStatMinLevel {
		return
	}

	if deps.StatAlloc != nil {
		deps.StatAlloc.AllocStat(sess, player, statName)
	}
}

// sendAbilityScores sends S_ABILITY_SCORES (opcode 174) — AC + elemental resistances.
// Matches Java S_OwnCharAttrDef.
func sendAbilityScores(sess *net.Session, p *world.PlayerInfo) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_ABILITY_SCORES)
	w.WriteC(byte(p.AC))
	w.WriteH(uint16(p.FireRes))
	w.WriteH(uint16(p.WaterRes))
	w.WriteH(uint16(p.WindRes))
	w.WriteH(uint16(p.EarthRes))
	sess.Send(w.Bytes())
}

// SendAbilityScores 匯出 sendAbilityScores — 供 system 套件發送 AC + 屬性抗性。
func SendAbilityScores(sess *net.Session, p *world.PlayerInfo) {
	sendAbilityScores(sess, p)
}

// SendRaiseAttrDialog 匯出 sendRaiseAttrDialog — 供 system 套件觸發屬性對話框。
func SendRaiseAttrDialog(sess *net.Session, charID int32) {
	sendRaiseAttrDialog(sess, charID)
}

// sendRaiseAttrDialog sends the "RaiseAttr" HTML dialog for bonus stat allocation.
// 格式對齊 S_NPCTalkReturn（Java）：3.80C 客戶端的 opcode 39 handler 一律讀取
// htmlID 之後的 writeH(flag) + writeH(count) 欄位。若缺少會讀到 padding 或
// 下一封包的 bytes，造成客戶端串流解析錯亂。
func sendRaiseAttrDialog(sess *net.Session, charID int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_HYPERTEXT)
	w.WriteD(charID)
	w.WriteS("RaiseAttr")
	w.WriteH(0) // data flag: 0 = 無額外資料（對齊 S_NPCTalkReturn 格式）
	w.WriteH(0) // data count: 0
	sess.Send(w.Bytes())
}
