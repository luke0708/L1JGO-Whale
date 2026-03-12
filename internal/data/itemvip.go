package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ItemVIP VIP 物品屬性定義。
type ItemVIP struct {
	ItemID int32 `yaml:"item_id"`
	Type   int   `yaml:"type"`

	// 六維加成
	AddStr int16 `yaml:"add_str"`
	AddDex int16 `yaml:"add_dex"`
	AddCon int16 `yaml:"add_con"`
	AddInt int16 `yaml:"add_int"`
	AddWis int16 `yaml:"add_wis"`
	AddCha int16 `yaml:"add_cha"`

	// 基礎屬性
	AddAC  int16 `yaml:"add_ac"`
	AddHP  int32 `yaml:"add_hp"`
	AddMP  int32 `yaml:"add_mp"`
	AddHPR int16 `yaml:"add_hpr"`
	AddMPR int16 `yaml:"add_mpr"`

	// 戰鬥加成
	AddDmg    int16 `yaml:"add_dmg"`
	AddHit    int16 `yaml:"add_hit"`
	AddBowDmg int16 `yaml:"add_bow_dmg"`
	AddBowHit int16 `yaml:"add_bow_hit"`
	AddDmgR   int16 `yaml:"add_dmg_r"`   // 傷害減免
	AddMagicR int16 `yaml:"add_magic_r"` // 魔法傷害減免
	AddMR     int16 `yaml:"add_mr"`
	AddSP     int16 `yaml:"add_sp"`

	// 元素抗性
	AddFire    int16 `yaml:"add_fire"`
	AddWind    int16 `yaml:"add_wind"`
	AddEarth   int16 `yaml:"add_earth"`
	AddWater   int16 `yaml:"add_water"`
	AddStun    int16 `yaml:"add_stun"`
	AddStone   int16 `yaml:"add_stone"`
	AddSleep   int16 `yaml:"add_sleep"`
	AddFreeze  int16 `yaml:"add_freeze"`
	AddSustain int16 `yaml:"add_sustain"`
	AddBlind   int16 `yaml:"add_blind"`

	// 經驗 & 死亡保護
	AddExp    float64 `yaml:"add_exp"`    // 經驗倍率加成
	DeathExp  bool    `yaml:"death_exp"`  // 死亡經驗保護
	DeathItem bool    `yaml:"death_item"` // 死亡物品保護
	DeathSkill bool   `yaml:"death_skill"` // 死亡技能保護

	// 特效
	SkinID  int32 `yaml:"skin_id"`
	GfxID   int32 `yaml:"gfx_id"`
	GfxTime int   `yaml:"gfx_time"` // 特效間隔秒
}

type itemVIPFile struct {
	Items []ItemVIP `yaml:"items"`
}

// ItemVIPTable VIP 物品資料表。
type ItemVIPTable struct {
	byItemID map[int32]*ItemVIP
}

// Get 根據物品 ID 取得 VIP 定義。
func (t *ItemVIPTable) Get(itemID int32) *ItemVIP {
	return t.byItemID[itemID]
}

// Count 回傳 VIP 物品數。
func (t *ItemVIPTable) Count() int {
	return len(t.byItemID)
}

// LoadItemVIPTable 載入 VIP 物品資料。
func LoadItemVIPTable(path string) (*ItemVIPTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ItemVIPTable{byItemID: make(map[int32]*ItemVIP)}, nil
		}
		return nil, fmt.Errorf("read item_vip: %w", err)
	}
	var f itemVIPFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse item_vip: %w", err)
	}
	t := &ItemVIPTable{byItemID: make(map[int32]*ItemVIP, len(f.Items))}
	for i := range f.Items {
		v := &f.Items[i]
		t.byItemID[v.ItemID] = v
	}
	return t, nil
}
