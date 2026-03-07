package system

import (
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// CharResetSystem 處理角色重置（洗點）邏輯。
// 實作 handler.CharResetManager 介面。
type CharResetSystem struct {
	deps *handler.Deps
}

// NewCharResetSystem 建立角色重置系統。
func NewCharResetSystem(deps *handler.Deps) *CharResetSystem {
	return &CharResetSystem{deps: deps}
}

// Start 啟動角色重置流程。由 NPC 動作 "ent" 觸發。
// Java: Npc_BaseReset2 — 檢查回憶蠟燭 → 清 buff → 凍結 → 初始化屬性。
func (s *CharResetSystem) Start(sess *net.Session, player *world.PlayerInfo) {
	if player.InCharReset {
		return
	}

	const resetItemCandle int32 = 49142

	// 檢查回憶蠟燭
	if player.Inv.FindByItemID(resetItemCandle) == nil {
		handler.SendServerMessage(sess, 1290) // "缺少必要道具。"
		return
	}

	classData := s.deps.Scripting.GetCharCreateData(int(player.ClassType))
	if classData == nil {
		return
	}

	// 計算目標等級（Java: maxLevel 計算）
	initTotal := 75 + int(player.ElixirStats)
	currentTotal := int(player.Str + player.Intel + player.Wis + player.Dex + player.Con + player.Cha)
	if player.Level > 50 {
		currentTotal += int(player.Level - 50 - player.BonusStats)
	}

	diff := currentTotal - initTotal
	var maxLevel int16
	if diff > 0 {
		maxLevel = int16(min(50+diff, 99))
	} else {
		maxLevel = player.Level
	}

	// 設定重置狀態
	player.InCharReset = true
	player.ResetMaxLevel = maxLevel
	player.ResetTempLevel = 1
	player.ResetElixirStats = player.ElixirStats

	// 重置屬性為職業基礎值
	player.Str = int16(classData.BaseSTR)
	player.Intel = int16(classData.BaseINT)
	player.Wis = int16(classData.BaseWIS)
	player.Dex = int16(classData.BaseDEX)
	player.Con = int16(classData.BaseCON)
	player.Cha = int16(classData.BaseCHA)

	// 重算初始 HP/MP
	initHP := s.deps.Scripting.CalcInitHP(int(player.ClassType), int(player.Con))
	initMP := s.deps.Scripting.CalcInitMP(int(player.ClassType), int(player.Wis))
	player.MaxHP = int16(initHP)
	player.MaxMP = int16(initMP)
	player.HP = player.MaxHP
	player.MP = player.MaxMP
	player.Level = 1

	// 凍結玩家
	handler.SendResetFreeze(sess, 4, true)

	// 發送狀態更新
	handler.SendPlayerStatus(sess, player)
	handler.SendAbilityScores(sess, player)
	handler.SendMagicStatus(sess, byte(player.SP), uint16(player.MR))

	// 發送 S_CharReset 格式 1（初始化 UI）
	handler.SendCharResetInit(sess, initHP, initMP, maxLevel)
}

// ResetStage1 處理初始屬性選擇。
// 客戶端送來 6 個 readC 作為初始點數分配（職業基礎 + 自由分配）。
func (s *CharResetSystem) ResetStage1(sess *net.Session, player *world.PlayerInfo,
	newStr, newInt, newWis, newDex, newCon, newCha int16) {

	classData := s.deps.Scripting.GetCharCreateData(int(player.ClassType))
	if classData == nil {
		return
	}

	// 驗證：每個屬性不得低於職業基礎值
	if newStr < int16(classData.BaseSTR) || newInt < int16(classData.BaseINT) ||
		newWis < int16(classData.BaseWIS) || newDex < int16(classData.BaseDEX) ||
		newCon < int16(classData.BaseCON) || newCha < int16(classData.BaseCHA) {
		return
	}

	// 驗證：總點數 = 職業基礎總和 + bonus
	baseTotal := classData.BaseSTR + classData.BaseDEX + classData.BaseCON +
		classData.BaseWIS + classData.BaseCHA + classData.BaseINT
	expectedTotal := baseTotal + classData.BonusAmount
	actualTotal := int(newStr + newInt + newWis + newDex + newCon + newCha)
	if actualTotal != expectedTotal {
		return
	}

	// 套用新屬性
	player.Str = newStr
	player.Intel = newInt
	player.Wis = newWis
	player.Dex = newDex
	player.Con = newCon
	player.Cha = newCha

	// 重算 HP/MP
	player.MaxHP = int16(s.deps.Scripting.CalcInitHP(int(player.ClassType), int(player.Con)))
	player.MaxMP = int16(s.deps.Scripting.CalcInitMP(int(player.ClassType), int(player.Wis)))
	player.HP = player.MaxHP
	player.MP = player.MaxMP

	// 設定 tempLevel = 1
	player.ResetTempLevel = 1
	player.Level = 1

	handler.SendCharResetLevel(sess, player)
}

// ResetStage2 處理逐級升級。
// type2: 0x00=升1級, 0x01-0x06=升1級+加屬性, 0x07=升10級, 0x08=完成。
func (s *CharResetSystem) ResetStage2(sess *net.Session, player *world.PlayerInfo, type2 byte) {
	switch type2 {
	case 0x00: // 升 1 級（不加屬性）
		if !s.resetLevelUp(player, 1) {
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x01: // STR +1 + 升 1 級
		player.Str++
		if !s.resetLevelUp(player, 1) {
			player.Str--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x02: // INT +1
		player.Intel++
		if !s.resetLevelUp(player, 1) {
			player.Intel--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x03: // WIS +1
		player.Wis++
		if !s.resetLevelUp(player, 1) {
			player.Wis--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x04: // DEX +1
		player.Dex++
		if !s.resetLevelUp(player, 1) {
			player.Dex--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x05: // CON +1
		player.Con++
		if !s.resetLevelUp(player, 1) {
			player.Con--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x06: // CHA +1
		player.Cha++
		if !s.resetLevelUp(player, 1) {
			player.Cha--
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x07: // 升 10 級
		remaining := player.ResetMaxLevel - player.ResetTempLevel
		if remaining < 10 {
			return
		}
		if !s.resetLevelUp(player, 10) {
			return
		}
		handler.SendCharResetLevel(sess, player)

	case 0x08: // 完成
		// 檢查萬能藥點數
		if player.ResetElixirStats > 0 {
			handler.SendCharResetElixir(sess, int(player.ResetElixirStats))
		} else {
			s.finishCharReset(sess, player)
		}
	}
}

// ResetStage2Finish 處理 stage2 完成時的最後屬性加點。
func (s *CharResetSystem) ResetStage2Finish(sess *net.Session, player *world.PlayerInfo, lastAttr byte) {
	s.applyLastAttr(player, lastAttr)

	if player.ResetElixirStats > 0 {
		handler.SendCharResetElixir(sess, int(player.ResetElixirStats))
	} else {
		s.finishCharReset(sess, player)
	}
}

// ResetStage3 處理萬能藥屬性覆寫。
func (s *CharResetSystem) ResetStage3(sess *net.Session, player *world.PlayerInfo,
	newStr, newInt, newWis, newDex, newCon, newCha int16) {

	player.Str = newStr
	player.Intel = newInt
	player.Wis = newWis
	player.Dex = newDex
	player.Con = newCon
	player.Cha = newCha

	s.finishCharReset(sess, player)
}

// ==================== 內部輔助 ====================

// resetLevelUp 執行 N 次升級的 HP/MP 增加。
func (s *CharResetSystem) resetLevelUp(player *world.PlayerInfo, levels int) bool {
	for i := 0; i < levels; i++ {
		if player.ResetTempLevel >= player.ResetMaxLevel {
			return false
		}
		player.ResetTempLevel++
		player.Level = player.ResetTempLevel

		result := s.deps.Scripting.CalcLevelUp(int(player.ClassType), int(player.Con), int(player.Wis))
		player.MaxHP += int16(result.HP)
		player.MaxMP += int16(result.MP)
	}
	player.HP = player.MaxHP
	player.MP = player.MaxMP
	return true
}

// applyLastAttr 套用最後一個屬性加點。
func (s *CharResetSystem) applyLastAttr(player *world.PlayerInfo, attr byte) {
	switch attr {
	case 0x01:
		player.Str++
	case 0x02:
		player.Intel++
	case 0x03:
		player.Wis++
	case 0x04:
		player.Dex++
	case 0x05:
		player.Con++
	case 0x06:
		player.Cha++
	}
}

// finishCharReset 完成角色重置：消耗回憶蠟燭、設定 BonusStats、傳送出去。
func (s *CharResetSystem) finishCharReset(sess *net.Session, player *world.PlayerInfo) {
	const resetItemCandle int32 = 49142
	const resetEndX int32 = 32628
	const resetEndY int32 = 32772
	const resetEndMapID int16 = 4

	player.InCharReset = false

	// 同步等級和經驗值
	player.Level = player.ResetTempLevel
	if s.deps.Scripting != nil {
		expResult := s.deps.Scripting.ExpForLevel(int(player.Level))
		player.Exp = int32(expResult)
	}

	// BonusStats = level - 50（若 > 50）
	if player.Level > 50 {
		player.BonusStats = player.Level - 50
	} else {
		player.BonusStats = 0
	}

	// 充滿 HP/MP
	player.HP = player.MaxHP
	player.MP = player.MaxMP

	// 消耗回憶蠟燭
	if candle := player.Inv.FindByItemID(resetItemCandle); candle != nil {
		removed := player.Inv.RemoveItem(candle.ObjectID, 1)
		if removed {
			handler.SendRemoveInventoryItem(sess, candle.ObjectID)
		} else {
			handler.SendItemCountUpdate(sess, candle)
		}
	}

	player.Dirty = true

	// 解除凍結
	handler.SendResetFreeze(sess, 4, false)

	// 發送更新封包
	handler.SendPlayerStatus(sess, player)
	handler.SendAbilityScores(sess, player)
	handler.SendMagicStatus(sess, byte(player.SP), uint16(player.MR))

	// 傳送至重置完成點
	if s.deps.World != nil {
		player.X = resetEndX
		player.Y = resetEndY
		player.MapID = resetEndMapID
		handler.SendMapID(sess, uint16(resetEndMapID), true)
		handler.SendOwnCharPackFromPlayer(sess, player)
	}

	// 重置暫存欄位
	player.ResetTempLevel = 0
	player.ResetMaxLevel = 0
	player.ResetElixirStats = 0
}
