package world

// HierarchInfo 隨身祭司的記憶體資料。
// 道具消耗型召喚獸 — 不持久化至 DB，伺服器重啟後消失。
// 只在遊戲迴圈 goroutine 中存取，不需要鎖。
type HierarchInfo struct {
	ID          int32  // NPC 範圍物件 ID（NextNpcID）
	OwnerCharID int32  // 主人的 CharID
	ItemObjID   int32  // 召喚此祭司的物品 ObjectID
	NpcID       int32  // 模板 NPC ID（220172-220175）
	Tier        int    // 等級 1-4（初級/中級/高級/頂級）
	GfxID       int32  // 圖形 ID
	Name        string // 顯示名稱
	NameID      string // 客戶端字串表索引鍵

	X       int32
	Y       int32
	MapID   int16
	Heading int16

	HP    int32
	MaxHP int32
	MP    int32
	MaxMP int32

	TimerTicks int // 剩餘存活 ticks
	MoveTimer  int // 移動冷卻
	BuffTimer  int // 自動增益冷卻（ticks）

	// 自動治療閾值：1-10，主人 HP < MaxHP * HealThreshold / 10 時治療
	HealThreshold int

	// 此等級可施放的 buff 技能 ID 列表
	BuffSkills []int32
}

// --- State 祭司管理 ---

// AddHierarch 註冊祭司到世界中。使用 NPC AOI 網格 + 實體碰撞。
func (s *State) AddHierarch(h *HierarchInfo) {
	if s.hierarchs == nil {
		s.hierarchs = make(map[int32]*HierarchInfo)
	}
	s.hierarchs[h.ID] = h
	s.npcAoi.Add(h.ID, h.X, h.Y, h.MapID)
	s.entity.Occupy(h.MapID, h.X, h.Y, h.ID)
}

// RemoveHierarch 從世界移除祭司並釋放格子。
func (s *State) RemoveHierarch(hID int32) *HierarchInfo {
	if s.hierarchs == nil {
		return nil
	}
	h, ok := s.hierarchs[hID]
	if !ok {
		return nil
	}
	s.npcAoi.Remove(h.ID, h.X, h.Y, h.MapID)
	s.entity.Vacate(h.MapID, h.X, h.Y, h.ID)
	delete(s.hierarchs, hID)
	return h
}

// GetHierarch 依物件 ID 查詢祭司。
func (s *State) GetHierarch(hID int32) *HierarchInfo {
	if s.hierarchs == nil {
		return nil
	}
	return s.hierarchs[hID]
}

// GetHierarchByOwner 取得玩家的祭司（最多 1 隻）。
func (s *State) GetHierarchByOwner(charID int32) *HierarchInfo {
	for _, h := range s.hierarchs {
		if h.OwnerCharID == charID {
			return h
		}
	}
	return nil
}

// UpdateHierarchPosition 移動祭司並更新 AOI + 實體網格。
func (s *State) UpdateHierarchPosition(hID int32, newX, newY int32, heading int16) {
	h := s.hierarchs[hID]
	if h == nil {
		return
	}
	oldX, oldY := h.X, h.Y
	h.X = newX
	h.Y = newY
	h.Heading = heading
	s.npcAoi.Move(hID, oldX, oldY, h.MapID, newX, newY, h.MapID)
	s.entity.Move(h.MapID, oldX, oldY, newX, newY, hID)
}

// TeleportHierarch 跨地圖傳送祭司。
func (s *State) TeleportHierarch(hID int32, newX, newY int32, newMapID, heading int16) {
	h := s.hierarchs[hID]
	if h == nil {
		return
	}
	oldX, oldY, oldMap := h.X, h.Y, h.MapID
	s.npcAoi.Remove(hID, oldX, oldY, oldMap)
	s.entity.Vacate(oldMap, oldX, oldY, hID)
	h.X = newX
	h.Y = newY
	h.MapID = newMapID
	h.Heading = heading
	s.npcAoi.Add(hID, newX, newY, newMapID)
	s.entity.Occupy(newMapID, newX, newY, hID)
}

// AllHierarchs 迭代所有在世祭司。
func (s *State) AllHierarchs(fn func(*HierarchInfo)) {
	for _, h := range s.hierarchs {
		fn(h)
	}
}

// HierarchCount 回傳在世祭司總數。
func (s *State) HierarchCount() int {
	return len(s.hierarchs)
}

// GetNearbyHierarchs 回傳指定位置附近的祭司（切比雪夫 <= 20）。
func (s *State) GetNearbyHierarchs(x, y int32, mapID int16) []*HierarchInfo {
	nearbyIDs := s.npcAoi.GetNearby(x, y, mapID)
	var result []*HierarchInfo
	for _, nid := range nearbyIDs {
		h := s.hierarchs[nid]
		if h == nil {
			continue
		}
		dx := h.X - x
		dy := h.Y - y
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		dist := dx
		if dy > dist {
			dist = dy
		}
		if dist <= 20 {
			result = append(result, h)
		}
	}
	return result
}
