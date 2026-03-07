package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// QuestTemplate 任務範本定義（從 YAML 載入）。
// Java: L1Quest + QuestExecutor 的 Go 資料驅動等價物。
type QuestTemplate struct {
	QuestID    int32  `yaml:"quest_id"`
	Name       string `yaml:"name"`
	MinLevel   int32  `yaml:"min_level"`
	ClassMask  int32  `yaml:"class_mask"` // 允許的職業位元遮罩：1=王族 2=騎士 4=精靈 8=法師 16=黑妖 32=龍騎 64=幻術 128=戰士
	Repeatable bool   `yaml:"repeatable"` // 是否可重複
	Enabled    bool   `yaml:"enabled"`
	Note       string `yaml:"note,omitempty"`
}

// CanAccept 檢查角色是否符合接任務條件（職業 + 等級）。
// classType: 0=王族 1=騎士 2=精靈 3=法師 4=黑妖 5=龍騎 6=幻術 7=戰士
func (q *QuestTemplate) CanAccept(classType int, level int32) bool {
	if !q.Enabled {
		return false
	}
	if level < q.MinLevel {
		return false
	}
	// classType 0-7 對應 bit 0-7
	if q.ClassMask != 0 {
		bit := int32(1) << uint(classType)
		if q.ClassMask&bit == 0 {
			return false
		}
	}
	return true
}

// QuestNpcDialog 任務 NPC 的對話定義 — 依任務進度顯示不同 HTML。
type QuestNpcDialog struct {
	NpcID   int32            `yaml:"npc_id"`
	QuestID int32            `yaml:"quest_id"`
	Dialogs []QuestStepEntry `yaml:"dialogs"` // 依 step 排序的對話
	Actions []QuestAction    `yaml:"actions"` // NPC 動作處理
}

// QuestStepEntry 特定步驟對應的 htmlid。
type QuestStepEntry struct {
	Step   int32  `yaml:"step"`    // 任務步驟（0=未開始, 255=已完成, -1=預設/fallback）
	HtmlID string `yaml:"html_id"` // 發送給客戶端的 htmlid 字串
}

// QuestAction 任務 NPC 動作處理定義。
type QuestAction struct {
	Cmd          string              `yaml:"cmd"`                     // 客戶端送來的動作字串
	RequiresStep int32               `yaml:"requires_step,omitempty"` // 需要的任務步驟（0=不檢查）
	MinLevel     int32               `yaml:"min_level,omitempty"`     // 最低等級（0=不檢查）
	ClassMask    int32               `yaml:"class_mask,omitempty"`    // 職業限制（0=不限）
	RequireItems []QuestItemRef      `yaml:"require_items,omitempty"` // 需持有的物品
	ConsumeItems []QuestItemRef      `yaml:"consume_items,omitempty"` // 扣除的物品
	GiveItems    []QuestItemRef      `yaml:"give_items,omitempty"`    // 給予的物品
	GiveExp      int32               `yaml:"give_exp,omitempty"`      // 給予經驗值
	GiveGold     int32               `yaml:"give_gold,omitempty"`     // 給予金幣
	SetStep      int32               `yaml:"set_step,omitempty"`      // 設定新步驟（0=不變）
	SuccessHtml  string              `yaml:"success_html,omitempty"`  // 成功後顯示的 htmlid
	FailHtml     string              `yaml:"fail_html,omitempty"`     // 失敗時顯示的 htmlid
	TeleportTo   *QuestTeleportDest  `yaml:"teleport_to,omitempty"`   // 傳送目的地
}

// QuestItemRef 任務物品引用。
type QuestItemRef struct {
	ItemID int32 `yaml:"item_id"`
	Count  int32 `yaml:"count"`
}

// QuestTeleportDest 任務傳送目的地。
type QuestTeleportDest struct {
	X     int32 `yaml:"x"`
	Y     int32 `yaml:"y"`
	MapID int16 `yaml:"map_id"`
}

// questFile YAML 根結構。
type questFile struct {
	Quests  []QuestTemplate  `yaml:"quests"`
	Dialogs []QuestNpcDialog `yaml:"dialogs"`
}

// QuestTable 任務資料索引表。
type QuestTable struct {
	templates map[int32]*QuestTemplate            // quest_id → template
	byNpc     map[int32][]*QuestNpcDialog          // npc_id → 該 NPC 關聯的任務對話列表
	byQuest   map[int32][]*QuestNpcDialog          // quest_id → 關聯的 NPC 對話列表
	npcAction map[questActionKey]*QuestAction       // (npc_id, cmd) → action
}

// questActionKey 是 NPC 動作查詢鍵。
type questActionKey struct {
	npcID int32
	cmd   string
}

// LoadQuestTable 從 YAML 載入任務範本與 NPC 對話定義。
func LoadQuestTable(path string) (*QuestTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("讀取任務資料: %w", err)
	}
	var f questFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析任務資料: %w", err)
	}

	t := &QuestTable{
		templates: make(map[int32]*QuestTemplate, len(f.Quests)),
		byNpc:     make(map[int32][]*QuestNpcDialog),
		byQuest:   make(map[int32][]*QuestNpcDialog),
		npcAction: make(map[questActionKey]*QuestAction),
	}

	for i := range f.Quests {
		q := &f.Quests[i]
		t.templates[q.QuestID] = q
	}

	for i := range f.Dialogs {
		d := &f.Dialogs[i]
		t.byNpc[d.NpcID] = append(t.byNpc[d.NpcID], d)
		t.byQuest[d.QuestID] = append(t.byQuest[d.QuestID], d)
		// 建立 action 快速查詢索引
		for j := range d.Actions {
			a := &d.Actions[j]
			key := questActionKey{npcID: d.NpcID, cmd: a.Cmd}
			t.npcAction[key] = a
		}
	}

	return t, nil
}

// GetQuest 依任務 ID 取得範本。
func (t *QuestTable) GetQuest(questID int32) *QuestTemplate {
	return t.templates[questID]
}

// GetNpcDialogs 取得指定 NPC 的所有任務對話定義。
func (t *QuestTable) GetNpcDialogs(npcID int32) []*QuestNpcDialog {
	return t.byNpc[npcID]
}

// GetNpcAction 查詢指定 NPC + 動作字串的任務動作定義。
func (t *QuestTable) GetNpcAction(npcID int32, cmd string) (*QuestNpcDialog, *QuestAction) {
	dialogs := t.byNpc[npcID]
	for _, d := range dialogs {
		for i := range d.Actions {
			if d.Actions[i].Cmd == cmd {
				return d, &d.Actions[i]
			}
		}
	}
	return nil, nil
}

// GetDialogHtmlID 根據玩家的任務進度取得對應的 htmlid。
// 優先匹配 exact step，fallback 到 step=-1（預設），最後回傳空字串。
func (d *QuestNpcDialog) GetDialogHtmlID(step int32) string {
	var fallback string
	for _, e := range d.Dialogs {
		if e.Step == step {
			return e.HtmlID
		}
		if e.Step == -1 {
			fallback = e.HtmlID
		}
	}
	return fallback
}

// Count 回傳任務範本總數。
func (t *QuestTable) Count() int {
	return len(t.templates)
}

// DialogCount 回傳 NPC 對話定義總數。
func (t *QuestTable) DialogCount() int {
	total := 0
	for _, ds := range t.byNpc {
		total += len(ds)
	}
	return total
}
