package system

// hierarch.go — 隨身祭司系統（召喚/解散）。
// Java: Magic_Hierarch.java — 道具觸發的自動增益型召喚獸。
// 實作 handler.HierarchManager interface。

import (
	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// HierarchSystem 實作 handler.HierarchManager。
type HierarchSystem struct {
	deps *handler.Deps
}

// NewHierarchSystem 建立 HierarchSystem。
func NewHierarchSystem(deps *handler.Deps) *HierarchSystem {
	return &HierarchSystem{deps: deps}
}

// UseHierarch 處理使用隨身祭司物品（切換行為：已召喚則解散，否則召喚）。
func (s *HierarchSystem) UseHierarch(sess *net.Session, player *world.PlayerInfo, invItem *world.InvItem, def *data.HierarchDef) {
	ws := s.deps.World

	// 切換：若此玩家已有祭司，解散舊的
	existing := ws.GetHierarchByOwner(player.CharID)
	if existing != nil {
		s.dismissHierarch(existing, player)
	}

	// 隱身中不可召喚（Java: Magic_Hierarch 檢查）
	if player.Invisible {
		return
	}

	// 建立 HierarchInfo
	h := &world.HierarchInfo{
		ID:            world.NextNpcID(),
		OwnerCharID:   player.CharID,
		ItemObjID:     invItem.ObjectID,
		NpcID:         def.NpcID,
		Tier:          def.Tier,
		GfxID:         def.GfxID,
		Name:          def.Name,
		NameID:        def.NameID,
		X:             player.X + int32(world.RandInt(3)) - 1,
		Y:             player.Y + int32(world.RandInt(3)) - 1,
		MapID:         player.MapID,
		Heading:       player.Heading,
		HP:            def.HP,
		MaxHP:         def.HP,
		MP:            0, // Java: 初始 MP = 0
		MaxMP:         def.MP,
		TimerTicks:    def.Duration * 5, // 秒 → ticks
		BuffTimer:     25,               // 初始等待 5 秒（25 ticks）再開始 buff
		HealThreshold: 3,                // 預設 30%（3/10）
		BuffSkills:    def.BuffSkills,
	}

	// 消耗 1 個物品
	removed := player.Inv.RemoveItem(invItem.ObjectID, 1)
	if removed {
		handler.SendRemoveInventoryItem(sess, invItem.ObjectID)
	} else {
		handler.SendItemCountUpdate(sess, invItem)
	}

	// 註冊到世界
	ws.AddHierarch(h)

	// 廣播外觀
	nearby := ws.GetNearbyPlayersAt(h.X, h.Y, h.MapID)
	for _, viewer := range nearby {
		handler.SendHierarchPack(viewer.Session, h, player.Name)
	}
	handler.SendHierarchPack(sess, h, player.Name)

	// 召喚音效
	handler.SendCompanionEffect(sess, h.ID, 5935)
}

// dismissHierarch 解散祭司。
func (s *HierarchSystem) dismissHierarch(h *world.HierarchInfo, player *world.PlayerInfo) {
	ws := s.deps.World

	// 從世界移除
	ws.RemoveHierarch(h.ID)

	// 廣播移除
	nearby := ws.GetNearbyPlayersAt(h.X, h.Y, h.MapID)
	for _, viewer := range nearby {
		handler.SendRemoveObject(viewer.Session, h.ID)
	}

	// 解散音效
	handler.SendCompanionEffect(player.Session, h.ID, 5936)
}
