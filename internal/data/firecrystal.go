package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FireCrystalEntry 火結晶表條目 — 每個道具在各強化等級下可分解出的結晶數量。
// Java: L1FireCrystal / L1FireSmithCrystalTable
type FireCrystalEntry struct {
	ItemID        int32  `yaml:"item_id"`
	Note          string `yaml:"note"`
	EnchantLevels [15]int32
}

// fireCrystalYAML 用於 YAML 解析的中間結構。
type fireCrystalYAML struct {
	ItemID        int32   `yaml:"item_id"`
	Note          string  `yaml:"note"`
	EnchantLevels []int32 `yaml:"enchant_levels"`
}

type fireCrystalFile struct {
	Items []fireCrystalYAML `yaml:"items"`
}

// GetCrystalCount 計算指定道具在特定強化等級下的結晶數量。
// Java: L1FireCrystal.get_CrystalCount()
// itemCategory: 1=武器, 2=防具（對應 Java type2）
// safeEnchant: 安定值（-1=不可強化, 0=安定0, 4+=安定4以上）
func (e *FireCrystalEntry) GetCrystalCount(enchantLvl int, itemCategory int, safeEnchant int) int32 {
	lvl := enchantLvl
	if lvl > 14 {
		lvl = 14
	}
	if lvl < 0 {
		lvl = 0
	}

	// 防具特殊處理（Java: L1FireCrystal.get_CrystalCount）
	if itemCategory == 2 { // 防具
		if safeEnchant >= 4 && lvl > 12 {
			// 安定4以上防具：強化值超過12時，以12計算
			lvl = 12
		} else if safeEnchant == 0 && lvl > 3 {
			// 安定0防具：強化值超過3時，以3計算
			lvl = 3
		} else if safeEnchant == -1 {
			// 不可強化防具：一律以0計算
			lvl = 0
		}
	}

	return e.EnchantLevels[lvl]
}

// FireCrystalTable 火結晶表 — 索引道具 ID 到結晶條目。
type FireCrystalTable struct {
	byItemID    map[int32]*FireCrystalEntry
	fireSmithID int32 // 火神煉化工匠 NPC ID
}

// LoadFireCrystalTable 從 YAML 檔案載入火結晶表。
func LoadFireCrystalTable(path string) (*FireCrystalTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fire_crystal_list: %w", err)
	}
	var f fireCrystalFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse fire_crystal_list: %w", err)
	}

	t := &FireCrystalTable{
		byItemID:    make(map[int32]*FireCrystalEntry, len(f.Items)),
		fireSmithID: 111414, // 火神煉化工匠 NPC ID
	}
	for _, item := range f.Items {
		entry := &FireCrystalEntry{
			ItemID: item.ItemID,
			Note:   item.Note,
		}
		for i := 0; i < 15 && i < len(item.EnchantLevels); i++ {
			entry.EnchantLevels[i] = item.EnchantLevels[i]
		}
		t.byItemID[item.ItemID] = entry
	}

	return t, nil
}

// Get 查詢指定道具 ID 的結晶條目。
func (t *FireCrystalTable) Get(itemID int32) *FireCrystalEntry {
	if t == nil {
		return nil
	}
	return t.byItemID[itemID]
}

// IsFireSmithNpc 檢查指定 NPC ID 是否為火神煉化工匠。
func (t *FireCrystalTable) IsFireSmithNpc(npcID int32) bool {
	if t == nil {
		return false
	}
	return npcID == t.fireSmithID
}

// Count 回傳已載入的結晶條目數量。
func (t *FireCrystalTable) Count() int {
	if t == nil {
		return 0
	}
	return len(t.byItemID)
}
