package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// HouseLocation 住宅靜態座標資料（從 YAML 載入，不可變）。
type HouseLocation struct {
	HouseID       int32 `yaml:"house_id"`
	KeeperID      int32 `yaml:"keeper_id"`       // 管家 NPC 模板 ID
	MapID         int16 `yaml:"map_id"`           // 所在地圖
	BasementMapID int16 `yaml:"basement_map_id"`  // 地下盟屋地圖（0=無）
	HomeX         int32 `yaml:"home_x"`           // 進入點 X
	HomeY         int32 `yaml:"home_y"`           // 進入點 Y
	X1            int32 `yaml:"x1"`               // 主範圍左上
	Y1            int32 `yaml:"y1"`
	X2            int32 `yaml:"x2"`               // 主範圍右下
	Y2            int32 `yaml:"y2"`
	X3            int32 `yaml:"x3"`               // 副範圍左上（不規則形狀用，0=無）
	Y3            int32 `yaml:"y3"`
	X4            int32 `yaml:"x4"`               // 副範圍右下
	Y4            int32 `yaml:"y4"`
}

// IsInBounds 判斷座標是否在此住宅範圍內。
func (h *HouseLocation) IsInBounds(x, y int32, mapID int16) bool {
	// 檢查地下盟屋地圖
	if h.BasementMapID != 0 && mapID == h.BasementMapID {
		return true
	}
	if mapID != h.MapID {
		return false
	}
	// 主範圍
	if h.X1 != 0 && x >= h.X1 && x <= h.X2 && y >= h.Y1 && y <= h.Y2 {
		return true
	}
	// 副範圍（不規則形狀）
	if h.X3 != 0 && x >= h.X3 && x <= h.X4 && y >= h.Y3 && y <= h.Y4 {
		return true
	}
	return false
}

// HouseTable 住宅靜態資料索引表。
type HouseTable struct {
	byID         map[int32]*HouseLocation
	byKeeper     map[int32]*HouseLocation // keeper NPC ID → house
	basementMaps map[int16]bool           // 地下盟屋 mapID 快速查詢
}

// LoadHouseTable 從 YAML 載入住宅靜態座標資料。
func LoadHouseTable(path string) (*HouseTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("讀取住宅資料: %w", err)
	}

	var file struct {
		Houses []HouseLocation `yaml:"houses"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("解析住宅資料: %w", err)
	}

	t := &HouseTable{
		byID:         make(map[int32]*HouseLocation, len(file.Houses)),
		byKeeper:     make(map[int32]*HouseLocation, len(file.Houses)),
		basementMaps: make(map[int16]bool),
	}
	for i := range file.Houses {
		h := &file.Houses[i]
		t.byID[h.HouseID] = h
		if h.KeeperID != 0 {
			t.byKeeper[h.KeeperID] = h
		}
		if h.BasementMapID != 0 {
			t.basementMaps[h.BasementMapID] = true
		}
	}

	return t, nil
}

// Get 依住宅 ID 取得靜態座標資料。
func (t *HouseTable) Get(houseID int32) *HouseLocation {
	return t.byID[houseID]
}

// GetByKeeper 依管家 NPC ID 取得對應的住宅。
func (t *HouseTable) GetByKeeper(keeperNpcID int32) *HouseLocation {
	return t.byKeeper[keeperNpcID]
}

// FindHouseAt 找出座標所在的住宅（無則回傳 nil）。
func (t *HouseTable) FindHouseAt(x, y int32, mapID int16) *HouseLocation {
	for _, h := range t.byID {
		if h.IsInBounds(x, y, mapID) {
			return h
		}
	}
	return nil
}

// IsHouseMap 判斷地圖 ID 是否為地下盟屋。
// Java: L1HouseLocation.isInHouse(short mapid) — mapID 5001~5123
func (t *HouseTable) IsHouseMap(mapID int16) bool {
	return t.basementMaps[mapID]
}

// Count 回傳住宅總數。
func (t *HouseTable) Count() int {
	return len(t.byID)
}
