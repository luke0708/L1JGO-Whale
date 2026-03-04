package data

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// CraftMaterial represents a required input material for a crafting recipe.
type CraftMaterial struct {
	ItemID     int32 `yaml:"item_id"`
	Amount     int32 `yaml:"amount"`
	EnchantLvl int32 `yaml:"enchant_lvl"` // 需要的強化等級（0 = 不限）
}

// CraftOutput represents a produced output item from a crafting recipe.
type CraftOutput struct {
	ItemID     int32 `yaml:"item_id"`
	Amount     int32 `yaml:"amount"`
	EnchantLvl int32 `yaml:"enchant_lvl"` // 成品強化值（0 = 無強化）
	Bless      int32 `yaml:"bless"`       // 0=無, 1=祝福, 2=詛咒
}

// CraftRecipe defines a single NPC crafting recipe.
type CraftRecipe struct {
	Action          string          `yaml:"action"`           // 動作字串（"craft0", "craft1"...）
	NpcID           int32           `yaml:"npc_id"`           // NPC ID（0 = any NPC）
	Note            string          `yaml:"note"`             // 配方名稱（顯示在清單中）
	AmountInputable bool            `yaml:"amount_inputable"` // 是否支援批量輸入
	AllInOnce       bool            `yaml:"all_in_once"`      // 批量時只判定一次機率

	// 限制條件
	RequiredLevel int32 `yaml:"required_level"` // 所需等級（0 = 不限）
	RequiredClass int32 `yaml:"required_class"` // 職業限制（0=全職, 1=王族, 2=騎士, 3=法師, 4=妖精, 5=黑妖, 6=龍騎, 7=幻術師, 8=戰士）
	HPConsume     int32 `yaml:"hp_consume"`     // 消耗 HP
	MPConsume     int32 `yaml:"mp_consume"`     // 消耗 MP

	// 機率系統
	SuccessRate int32 `yaml:"success_rate"` // 成功率 0-100（0 = 100% 成功）

	// 輸入/輸出
	Items     []CraftOutput   `yaml:"items"`     // 成品
	Materials []CraftMaterial `yaml:"materials"` // 材料

	// 成功時額外獎勵
	BonusItemID     int32 `yaml:"bonus_item_id"`     // 額外獎勵物品 ID（0 = 無）
	BonusItemCount  int32 `yaml:"bonus_item_count"`  // 額外獎勵數量

	// 失敗時殘留
	ResidueItemID    int32 `yaml:"residue_item_id"`    // 殘留物品 ID（0 = 無）
	ResidueItemCount int32 `yaml:"residue_item_count"` // 殘留數量

	// UI
	Broadcast bool `yaml:"broadcast"` // 成功時全服廣播
}

type itemMakingFile struct {
	Recipes []CraftRecipe `yaml:"recipes"`
}

// ItemMakingTable stores crafting recipes indexed by action string and NPC ID.
type ItemMakingTable struct {
	byAction    map[string]*CraftRecipe  // action → recipe（無 NPC 綁定的簡易查詢）
	byNpcAction map[string]*CraftRecipe  // "{npcID}_{action}" → recipe（NPC 專屬查詢）
	byNpcID     map[int32][]*CraftRecipe // npcID → 該 NPC 的所有配方（按 action 排序）
}

// LoadItemMakingTable loads crafting recipes from a YAML file.
func LoadItemMakingTable(path string) (*ItemMakingTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read item_making_list: %w", err)
	}
	var f itemMakingFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse item_making_list: %w", err)
	}

	t := &ItemMakingTable{
		byAction:    make(map[string]*CraftRecipe, len(f.Recipes)),
		byNpcAction: make(map[string]*CraftRecipe, len(f.Recipes)),
		byNpcID:     make(map[int32][]*CraftRecipe),
	}
	for i := range f.Recipes {
		r := &f.Recipes[i]
		t.byAction[r.Action] = r
		if r.NpcID > 0 {
			// 複合 key：Java L1BlendTable 用 "{npcid}{action}" 作為索引
			key := fmt.Sprintf("%d_%s", r.NpcID, r.Action)
			t.byNpcAction[key] = r
			t.byNpcID[r.NpcID] = append(t.byNpcID[r.NpcID], r)
		}
	}

	// 按 action 排序每個 NPC 的配方列表（A < B < C... < Z < a1 < a2...）
	for _, recipes := range t.byNpcID {
		sort.Slice(recipes, func(i, j int) bool {
			return craftActionOrder(recipes[i].Action) < craftActionOrder(recipes[j].Action)
		})
	}

	return t, nil
}

// Get returns the recipe for the given action string, or nil if not found.
func (t *ItemMakingTable) Get(action string) *CraftRecipe {
	if t == nil {
		return nil
	}
	return t.byAction[action]
}

// GetByNpcAction 查詢特定 NPC 的配方（複合 key）。
// Java: L1BlendTable._CraftIndex 用 "{npcid}{action}" 作為 key。
func (t *ItemMakingTable) GetByNpcAction(npcID int32, action string) *CraftRecipe {
	if t == nil {
		return nil
	}
	key := fmt.Sprintf("%d_%s", npcID, action)
	return t.byNpcAction[key]
}

// craftActionOrder 將配方 action 字串（A-Z, a1-a17）轉換為排序用數值。
// A=0, B=1, ..., Z=25, a1=26, a2=27, ...
func craftActionOrder(action string) int {
	if len(action) == 1 && action[0] >= 'A' && action[0] <= 'Z' {
		return int(action[0] - 'A')
	}
	if len(action) >= 2 && action[0] == 'a' {
		n := 0
		for _, c := range action[1:] {
			n = n*10 + int(c-'0')
		}
		return 26 + n - 1 // a1=26, a2=27, ...
	}
	return 999 // 未知排序放最後
}

// GetByNpcID returns all recipes for the given NPC ID, sorted by action.
func (t *ItemMakingTable) GetByNpcID(npcID int32) []*CraftRecipe {
	if t == nil {
		return nil
	}
	return t.byNpcID[npcID]
}

// GetByNpcIndex 按索引查詢特定 NPC 的配方（0-based）。
// 用於火神合成 UI（C_PledgeContent type=14），客戶端以數字索引選擇配方。
func (t *ItemMakingTable) GetByNpcIndex(npcID int32, index int) *CraftRecipe {
	if t == nil {
		return nil
	}
	recipes := t.byNpcID[npcID]
	if index >= 0 && index < len(recipes) {
		return recipes[index]
	}
	return nil
}

// Count returns the total number of loaded recipes.
func (t *ItemMakingTable) Count() int {
	if t == nil {
		return 0
	}
	return len(t.byAction)
}
