package system

// pet_match.go — 寵物比賽系統。
// Java: L1PetMatch.java — 單例管理器 + 計時器，最多 1 場並行比賽。
// Go: tick-based，在 CompanionAISystem 中每 tick 呼叫 TickPetMatches()。

import (
	"context"
	"fmt"
	"time"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// 寵物比賽常數（與 Java L1PetMatch 一致）。
const (
	petMatchMaxSlots   = 1     // 最大並行比賽數
	petMatchArenaX     = 32799 // 競技場進入座標
	petMatchArenaY     = 32868
	petMatchReturnX    = 32630 // 比賽結束返回座標
	petMatchReturnY    = 32744
	petMatchReturnMap  = 4     // 返回地圖 ID
	petMatchTimeout    = 1500  // 超時 tick 數（5 分鐘 = 300 秒 / 0.2 秒/tick）
	petMatchReadyDelay = 15    // 就緒延遲 tick 數（3 秒 = 15 ticks @ 200ms）
	petMatchMedalID    = 41309 // 獎牌物品 ID
)

// 競技場地圖 ID 陣列（Java: PET_MATCH_MAPID）。
var petMatchMapIDs = [petMatchMaxSlots]int16{5125}

// petMatchStatus 比賽狀態。
const (
	pmStatusNone    = 0 // 空閒
	pmStatusReady1  = 1 // 玩家 1 已進入
	pmStatusReady2  = 2 // 玩家 2 已進入（玩家 1 離開）
	pmStatusPlaying = 3 // 比賽進行中
)

// PetMatchSlot 單場比賽的完整狀態。
type PetMatchSlot struct {
	Status int

	Pc1CharID int32
	Pc2CharID int32
	Pet1ID    int32 // 世界中的寵物 object ID
	Pet2ID    int32

	ReadyTick int // 就緒階段已等待的 tick 數
	MatchTick int // 比賽進行的 tick 數
}

// PetMatchSystem 實作 handler.PetMatchManager。
type PetMatchSystem struct {
	deps  *handler.Deps
	slots [petMatchMaxSlots]PetMatchSlot
}

// NewPetMatchSystem 建立寵物比賽系統。
func NewPetMatchSystem(deps *handler.Deps) *PetMatchSystem {
	return &PetMatchSystem{deps: deps}
}

// EnterPetMatch 處理玩家報名寵物比賽。
// Java: L1PetMatch.enterPetMatch() + Npc_PetWar.action()。
func (s *PetMatchSystem) EnterPetMatch(sess *net.Session, player *world.PlayerInfo, amuletObjID int32) bool {
	ws := s.deps.World

	// 驗證：玩家不得有已召喚的寵物（Java: petlist.length > 0）
	if len(ws.GetPetsByOwner(player.CharID)) > 0 {
		handler.SendServerMessage(sess, 1187) // "寵物項鍊正在使用中。"
		return false
	}

	// 尋找可用的比賽槽位
	slotIdx := s.findAvailableSlot()
	if slotIdx < 0 {
		handler.SendServerMessage(sess, 1182) // "遊戲已經開始了"
		return false
	}

	// 從 DB 載入寵物資料
	pet := s.withdrawPet(sess, player, amuletObjID)
	if pet == nil {
		return false
	}

	// 傳送到競技場
	handler.TeleportPlayer(sess, player, petMatchArenaX, petMatchArenaY,
		petMatchMapIDs[slotIdx], 0, s.deps)

	// 更新槽位
	slot := &s.slots[slotIdx]
	status := s.getSlotStatus(slotIdx)
	switch status {
	case pmStatusNone:
		slot.Pc1CharID = player.CharID
		slot.Pet1ID = pet.ID
		slot.Status = pmStatusReady1
		slot.ReadyTick = 0
		slot.MatchTick = 0
	case pmStatusReady1:
		slot.Pc2CharID = player.CharID
		slot.Pet2ID = pet.ID
		slot.Status = pmStatusPlaying
	case pmStatusReady2:
		slot.Pc1CharID = player.CharID
		slot.Pet1ID = pet.ID
		slot.Status = pmStatusPlaying
	}

	// 如果兩位玩家都就位 → 開始比賽
	if slot.Status == pmStatusPlaying {
		s.startMatch(slotIdx)
	}

	return true
}

// TickPetMatches 每 tick 呼叫，處理所有比賽槽位的狀態更新。
func (s *PetMatchSystem) TickPetMatches() {
	for i := 0; i < petMatchMaxSlots; i++ {
		slot := &s.slots[i]
		switch slot.Status {
		case pmStatusReady1, pmStatusReady2:
			s.tickReady(i, slot)
		case pmStatusPlaying:
			s.tickPlaying(i, slot)
		}
	}
}

// ==================== 內部方法 ====================

// findAvailableSlot 尋找可用的比賽槽位（優先找有對手等待的）。
// Java: decidePetMatchNo() — 先找有等待的，再找空閒的。
func (s *PetMatchSystem) findAvailableSlot() int {
	// 優先找有玩家等待的槽位（可以配對）
	for i := 0; i < petMatchMaxSlots; i++ {
		status := s.getSlotStatus(i)
		if status == pmStatusReady1 || status == pmStatusReady2 {
			return i
		}
	}
	// 找空閒槽位
	for i := 0; i < petMatchMaxSlots; i++ {
		status := s.getSlotStatus(i)
		if status == pmStatusNone {
			return i
		}
	}
	return -1
}

// getSlotStatus 取得槽位的實際狀態（檢查玩家是否仍在競技場）。
// Java: getPetMatchStatus() — 檢查玩家是否在線上且在競技場地圖。
func (s *PetMatchSystem) getSlotStatus(idx int) int {
	slot := &s.slots[idx]
	ws := s.deps.World
	mapID := petMatchMapIDs[idx]

	pc1 := ws.GetByCharID(slot.Pc1CharID)
	pc2 := ws.GetByCharID(slot.Pc2CharID)

	if pc1 == nil && pc2 == nil {
		s.clearSlot(idx)
		return pmStatusNone
	}
	if pc1 == nil && pc2 != nil {
		if pc2.MapID == mapID {
			return pmStatusReady2
		}
		s.clearSlot(idx)
		return pmStatusNone
	}
	if pc1 != nil && pc2 == nil {
		if pc1.MapID == mapID {
			return pmStatusReady1
		}
		s.clearSlot(idx)
		return pmStatusNone
	}
	// 雙方都在競技場
	if pc1.MapID == mapID && pc2.MapID == mapID {
		return pmStatusPlaying
	}
	if pc1.MapID == mapID {
		slot.Pc2CharID = 0
		slot.Pet2ID = 0
		return pmStatusReady1
	}
	if pc2.MapID == mapID {
		slot.Pc1CharID = 0
		slot.Pet1ID = 0
		return pmStatusReady2
	}
	s.clearSlot(idx)
	return pmStatusNone
}

// withdrawPet 從 DB 載入寵物並生成到世界中（Java: withdrawPet）。
func (s *PetMatchSystem) withdrawPet(sess *net.Session, player *world.PlayerInfo, amuletObjID int32) *world.PetInfo {
	if s.deps.PetRepo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	petRow, err := s.deps.PetRepo.LoadByItemObjID(ctx, amuletObjID)
	if err != nil || petRow == nil {
		return nil
	}

	tmpl := s.deps.Npcs.Get(petRow.NpcID)
	if tmpl == nil {
		return nil
	}

	maxHP := petRow.MaxHP
	if maxHP <= 0 {
		maxHP = petRow.HP
	}
	if maxHP <= 0 {
		maxHP = tmpl.HP
	}
	maxMP := petRow.MaxMP
	if maxMP <= 0 {
		maxMP = petRow.MP
	}
	if maxMP <= 0 {
		maxMP = tmpl.MP
	}

	// 滿血召喚（Java 行為）
	pet := &world.PetInfo{
		ID:          world.NextNpcID(),
		OwnerCharID: player.CharID,
		ItemObjID:   amuletObjID,
		NpcID:       petRow.NpcID,
		Name:        petRow.Name,
		Level:       petRow.Level,
		HP:          maxHP,
		MaxHP:       maxHP,
		MP:          maxMP,
		MaxMP:       maxMP,
		Exp:         petRow.Exp,
		Lawful:      petRow.Lawful,
		GfxID:       tmpl.GfxID,
		NameID:      tmpl.NameID,
		MoveSpeed:   tmpl.PassiveSpeed,
		X:           player.X + int32(world.RandInt(3)) - 1,
		Y:           player.Y + int32(world.RandInt(3)) - 1,
		MapID:       player.MapID,
		Heading:     player.Heading,
		Status:      world.PetStatusRest,
		AC:          tmpl.AC,
		STR:         tmpl.STR,
		DEX:         tmpl.DEX,
		MR:          tmpl.MR,
		AtkDmg:      tmpl.HP / 4,
		AtkSpeed:    tmpl.AtkSpeed,
		Ranged:      tmpl.Ranged,
	}

	ws := s.deps.World
	ws.AddPet(pet)

	// 廣播外觀
	nearby := ws.GetNearbyPlayersAt(pet.X, pet.Y, pet.MapID)
	for _, viewer := range nearby {
		isOwner := viewer.CharID == player.CharID
		handler.SendPetPack(viewer.Session, pet, isOwner, player.Name)
	}

	// 寵物比賽不發送控制面板（寵物自主行動）
	handler.SendPetHpMeter(sess, pet.ID, pet.HP, pet.MaxHP)

	return pet
}

// startMatch 開始比賽：設定雙方寵物為攻擊模式，互相設定為目標。
// Java: startPetMatch() — setCurrentPetStatus(1) + setTarget()。
func (s *PetMatchSystem) startMatch(idx int) {
	slot := &s.slots[idx]
	ws := s.deps.World

	pet1 := ws.GetPet(slot.Pet1ID)
	pet2 := ws.GetPet(slot.Pet2ID)
	if pet1 == nil || pet2 == nil {
		s.endMatch(idx, 3)
		return
	}

	pet1.Status = world.PetStatusAggressive
	pet1.AggroPetID = pet2.ID

	pet2.Status = world.PetStatusAggressive
	pet2.AggroPetID = pet1.ID

	slot.MatchTick = 0
}

// tickReady 就緒階段：等待對手。
// Java 中無 ready 超時機制，但為避免玩家永遠卡在競技場，加入 60 秒超時。
func (s *PetMatchSystem) tickReady(idx int, slot *PetMatchSlot) {
	slot.ReadyTick++
	// 超過 60 秒（300 ticks）仍無對手 → 取消，不發獎牌
	if slot.ReadyTick > 300 {
		s.cancelMatch(idx)
	}
}

// tickPlaying 比賽進行中：檢查寵物死亡和超時。
func (s *PetMatchSystem) tickPlaying(idx int, slot *PetMatchSlot) {
	slot.MatchTick++
	ws := s.deps.World

	pet1 := ws.GetPet(slot.Pet1ID)
	pet2 := ws.GetPet(slot.Pet2ID)

	// 檢查寵物死亡
	pet1Dead := pet1 == nil || pet1.Dead
	pet2Dead := pet2 == nil || pet2.Dead

	if pet1Dead || pet2Dead {
		winner := 3 // 平手
		if !pet1Dead && pet2Dead {
			winner = 1
		} else if pet1Dead && !pet2Dead {
			winner = 2
		}
		s.endMatch(idx, winner)
		return
	}

	// 超時 → 平手
	if slot.MatchTick >= petMatchTimeout {
		s.endMatch(idx, 3)
	}
}

// endMatch 結束比賽：發放獎勵、傳送回城、清理。
// Java: endPetMatch() + giveMedal() + qiutPetMatch()。
// winNo: 1=寵物1贏, 2=寵物2贏, 3=平手。
func (s *PetMatchSystem) endMatch(idx int, winNo int) {
	slot := &s.slots[idx]
	ws := s.deps.World

	pc1 := ws.GetByCharID(slot.Pc1CharID)
	pc2 := ws.GetByCharID(slot.Pc2CharID)

	// 發放獎牌（Java: giveMedal）
	switch winNo {
	case 1:
		s.giveMedal(pc1, idx, true)
		s.giveMedal(pc2, idx, false)
	case 2:
		s.giveMedal(pc1, idx, false)
		s.giveMedal(pc2, idx, true)
	case 3:
		// 平手：都不發獎牌（Java: giveMedal with isWin=false 也會發 1 個，但只有正式結束才發）
		// 但 Java 中 winNo==3 仍呼叫 giveMedal(false) 發 1 個。保持一致。
		s.giveMedal(pc1, idx, false)
		s.giveMedal(pc2, idx, false)
	}

	// 清理寵物 + 傳送回城（Java: qiutPetMatch）
	s.cleanupPlayer(pc1, idx)
	s.cleanupPlayer(pc2, idx)

	s.clearSlot(idx)
}

// giveMedal 發放獎牌。
// Java: 勝者 3 個、敗者 1 個（物品 41309）。
func (s *PetMatchSystem) giveMedal(pc *world.PlayerInfo, slotIdx int, isWin bool) {
	if pc == nil {
		return
	}
	if pc.MapID != petMatchMapIDs[slotIdx] {
		return
	}

	if isWin {
		// 廣播勝利訊息（Java: S_ServerMessage(1166, pcName)）
		handler.SendServerMessageStr(pc.Session, 1166, pc.Name)
	}

	count := int32(1) // 敗者/平手 1 個
	if isWin {
		count = 3 // 勝者 3 個
	}

	// 給予獎牌物品
	if s.deps.Items == nil {
		return
	}
	tmpl := s.deps.Items.Get(petMatchMedalID)
	if tmpl == nil {
		return
	}
	invItem := pc.Inv.AddItem(petMatchMedalID, count, tmpl.Name, tmpl.InvGfx, tmpl.Weight, true, 0)
	handler.SendAddItem(pc.Session, invItem)
	handler.SendServerMessageStr(pc.Session, 403, fmt.Sprintf("%s (%d)", tmpl.Name, count))
}

// cleanupPlayer 清理玩家的比賽寵物並傳送回城。
func (s *PetMatchSystem) cleanupPlayer(pc *world.PlayerInfo, slotIdx int) {
	if pc == nil {
		return
	}
	ws := s.deps.World

	// 移除所有在競技場的寵物（不儲存到 DB — 比賽不影響寵物資料）
	if pc.MapID == petMatchMapIDs[slotIdx] {
		pets := ws.GetPetsByOwner(pc.CharID)
		for _, pet := range pets {
			ws.RemovePet(pet.ID)
			nearby := ws.GetNearbyPlayersAt(pet.X, pet.Y, pet.MapID)
			for _, viewer := range nearby {
				handler.SendRemoveObject(viewer.Session, pet.ID)
			}
			handler.SendPetCtrlMenu(pc.Session, pet, false)
		}

		// 傳送回城
		handler.TeleportPlayer(pc.Session, pc, petMatchReturnX, petMatchReturnY,
			petMatchReturnMap, 4, s.deps)
	}
}

// cancelMatch 取消比賽（ready 超時、無對手），不發獎牌。
func (s *PetMatchSystem) cancelMatch(idx int) {
	slot := &s.slots[idx]
	ws := s.deps.World

	pc1 := ws.GetByCharID(slot.Pc1CharID)
	pc2 := ws.GetByCharID(slot.Pc2CharID)

	s.cleanupPlayer(pc1, idx)
	s.cleanupPlayer(pc2, idx)
	s.clearSlot(idx)
}

// clearSlot 清空比賽槽位。
func (s *PetMatchSystem) clearSlot(idx int) {
	s.slots[idx] = PetMatchSlot{}
}
