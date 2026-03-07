package system

import (
	"strings"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// GMCommandSystem 處理 GM 命令的角色狀態修改。
// 實作 handler.GMCommandManager 介面。
type GMCommandSystem struct {
	deps *handler.Deps
}

// NewGMCommandSystem 建立 GM 命令系統。
func NewGMCommandSystem(deps *handler.Deps) *GMCommandSystem {
	return &GMCommandSystem{deps: deps}
}

// SetLevel 設定玩家等級（含經驗值、HP/MP 重算）。
func (s *GMCommandSystem) SetLevel(sess *net.Session, player *world.PlayerInfo, level int) {
	player.Level = int16(level)
	player.Exp = int32(s.deps.Scripting.ExpForLevel(level))

	baseHP, baseMP := calcBaseHPMP(player.ClassType, player.Level, player.Con, player.Wis, s.deps)
	player.MaxHP = baseHP
	player.MaxMP = baseMP
	player.HP = player.MaxHP
	player.MP = player.MaxMP

	handler.SendPlayerStatus(sess, player)
	handler.SendExpUpdate(sess, player.Level, player.Exp)
	handler.SendHpUpdate(sess, player)
	handler.SendMpUpdate(sess, player)
}

// SetHP 設定玩家 HP（含死亡復活處理）。
func (s *GMCommandSystem) SetHP(sess *net.Session, player *world.PlayerInfo, hp int) {
	player.HP = int16(hp)
	if player.HP > player.MaxHP {
		player.MaxHP = player.HP
	}
	if player.HP > 0 && player.Dead {
		player.Dead = false
		player.LastMoveTime = 0
		s.deps.World.OccupyEntity(player.MapID, player.X, player.Y, player.CharID)
	}
	handler.SendHpUpdate(sess, player)
	handler.SendPlayerStatus(sess, player)
}

// SetMP 設定玩家 MP。
func (s *GMCommandSystem) SetMP(sess *net.Session, player *world.PlayerInfo, mp int) {
	player.MP = int16(mp)
	if player.MP > player.MaxMP {
		player.MaxMP = player.MP
	}
	handler.SendMpUpdate(sess, player)
	handler.SendPlayerStatus(sess, player)
}

// FullHeal 補滿 HP/MP（含死亡復活處理）。
func (s *GMCommandSystem) FullHeal(sess *net.Session, player *world.PlayerInfo) {
	player.HP = player.MaxHP
	player.MP = player.MaxMP
	if player.Dead {
		player.Dead = false
		player.LastMoveTime = 0
		s.deps.World.OccupyEntity(player.MapID, player.X, player.Y, player.CharID)
	}
	handler.SendHpUpdate(sess, player)
	handler.SendMpUpdate(sess, player)
}

// SetStat 設定指定屬性值。
func (s *GMCommandSystem) SetStat(sess *net.Session, player *world.PlayerInfo, stat string, value int16) {
	switch strings.ToLower(stat) {
	case "str":
		player.Str = value
	case "dex":
		player.Dex = value
	case "con":
		player.Con = value
	case "wis":
		player.Wis = value
	case "int":
		player.Intel = value
	case "cha":
		player.Cha = value
	}
	handler.SendPlayerStatus(sess, player)
}

// GiveItem 給予物品。
func (s *GMCommandSystem) GiveItem(sess *net.Session, player *world.PlayerInfo, itemID, count int32, enchant int8) {
	itemInfo := s.deps.Items.Get(itemID)
	if itemInfo == nil {
		return
	}

	stackable := itemInfo.Stackable || itemID == world.AdenaItemID
	existing := player.Inv.FindByItemID(itemID)
	wasExisting := existing != nil && stackable

	invItem := player.Inv.AddItem(
		itemID, count, itemInfo.Name, itemInfo.InvGfx,
		itemInfo.Weight, stackable, byte(itemInfo.Bless),
	)
	invItem.EnchantLvl = enchant
	invItem.UseType = itemInfo.UseTypeID

	if wasExisting {
		handler.SendItemCountUpdate(sess, invItem)
	} else {
		handler.SendAddItem(sess, invItem)
	}
	handler.SendWeightUpdate(sess, player)
}

// GiveGold 給予金幣。
func (s *GMCommandSystem) GiveGold(sess *net.Session, player *world.PlayerInfo, amount int32) {
	adenaInfo := s.deps.Items.Get(world.AdenaItemID)
	if adenaInfo == nil {
		return
	}

	existing := player.Inv.FindByItemID(world.AdenaItemID)
	wasExisting := existing != nil

	invItem := player.Inv.AddItem(
		world.AdenaItemID, amount, adenaInfo.Name, adenaInfo.InvGfx,
		0, true, byte(adenaInfo.Bless),
	)

	if wasExisting {
		handler.SendItemCountUpdate(sess, invItem)
	} else {
		handler.SendAddItem(sess, invItem)
	}
	handler.SendWeightUpdate(sess, player)
}

// calcBaseHPMP 計算指定等級的基礎 HP/MP（透過 Lua 重複升級計算）。
func calcBaseHPMP(classType int16, level int16, con, wis int16, deps *handler.Deps) (int16, int16) {
	hp := int16(deps.Scripting.CalcInitHP(int(classType), int(con)))
	mp := int16(deps.Scripting.CalcInitMP(int(classType), int(wis)))

	for lv := int16(2); lv <= level; lv++ {
		result := deps.Scripting.CalcLevelUp(int(classType), int(con), int(wis))
		hp += int16(result.HP)
		mp += int16(result.MP)
	}
	return hp, mp
}
