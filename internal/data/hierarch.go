package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// HierarchDef 隨身祭司的靜態定義（從 YAML 載入）。
type HierarchDef struct {
	ItemID     int32   // 觸發召喚的道具 ID（39007-39010）
	NpcID      int32   // 模板 NPC ID
	GfxID      int32   // 圖形 ID
	Name       string  // 顯示名稱
	NameID     string  // 客戶端字串索引鍵
	Tier       int     // 等級（1-4）
	HP         int32   // 最大 HP
	MP         int32   // 最大 MP
	Duration   int     // 持續時間（秒）
	BuffSkills []int32 // 可施放的 buff 技能 ID 列表
}

// HierarchTable 隨身祭司查找表，key = 道具 item_id。
type HierarchTable struct {
	defs map[int32]*HierarchDef
}

// Get 依道具 ID 查詢祭司定義，無則回傳 nil。
func (t *HierarchTable) Get(itemID int32) *HierarchDef {
	if t == nil {
		return nil
	}
	return t.defs[itemID]
}

// Count 回傳已載入的祭司定義數量。
func (t *HierarchTable) Count() int {
	if t == nil {
		return 0
	}
	return len(t.defs)
}

// --- YAML 載入 ---

type hierarchEntry struct {
	ItemID     int32   `yaml:"item_id"`
	NpcID      int32   `yaml:"npc_id"`
	GfxID      int32   `yaml:"gfx_id"`
	Name       string  `yaml:"name"`
	NameID     string  `yaml:"name_id"`
	Tier       int     `yaml:"tier"`
	HP         int32   `yaml:"hp"`
	MP         int32   `yaml:"mp"`
	Duration   int     `yaml:"duration"`
	BuffSkills []int32 `yaml:"buff_skills"`
}

type hierarchFile struct {
	Hierarchs []hierarchEntry `yaml:"hierarchs"`
}

// LoadHierarchTable 從 YAML 檔案載入隨身祭司資料。
func LoadHierarchTable(path string) (*HierarchTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("讀取隨身祭司資料失敗: %w", err)
	}
	var f hierarchFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析隨身祭司 YAML 失敗: %w", err)
	}
	t := &HierarchTable{defs: make(map[int32]*HierarchDef, len(f.Hierarchs))}
	for i := range f.Hierarchs {
		e := &f.Hierarchs[i]
		t.defs[e.ItemID] = &HierarchDef{
			ItemID:     e.ItemID,
			NpcID:      e.NpcID,
			GfxID:      e.GfxID,
			Name:       e.Name,
			NameID:     e.NameID,
			Tier:       e.Tier,
			HP:         e.HP,
			MP:         e.MP,
			Duration:   e.Duration,
			BuffSkills: e.BuffSkills,
		}
	}
	return t, nil
}
