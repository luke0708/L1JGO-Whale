package world

import "sync/atomic"

// npcIDCounter generates unique NPC object IDs.
// Starts at 200_000_000 to avoid collision with character DB IDs.
var npcIDCounter atomic.Int32

func init() {
	npcIDCounter.Store(200_000_000)
}

// NextNpcID returns a unique object ID for an NPC instance.
func NextNpcID() int32 {
	return npcIDCounter.Add(1)
}

// NpcInfo holds runtime data for an NPC currently in-world.
// Accessed only from the game loop goroutine — no locks.
type NpcInfo struct {
	ID      int32 // unique object ID (from NextNpcID)
	NpcID   int32 // template ID
	Impl    string // L1Monster, L1Merchant, L1Guard, etc.
	GfxID   int32
	Name    string
	NameID  string // client string table key (e.g. "$936")
	Level   int16
	X       int32
	Y       int32
	MapID   int16
	Heading int16
	HP      int32
	MaxHP   int32
	MP      int32
	MaxMP   int32
	AC      int16
	STR     int16
	DEX     int16
	Exp     int32  // exp reward on kill
	Lawful  int32
	Size    string // "small" or "large"
	MR      int16
	Undead  bool
	Agro    bool   // true = aggressive, attacks players on sight
	AtkDmg  int32  // damage per attack (simplified: Level + STR/3)
	Ranged  int16  // attack range (1 = melee, >1 = ranged attacker)
	AtkSpeed   int16 // attack animation speed (ms, 0 = default)
	MoveSpeed  int16 // passive/move speed (ms, 0 = default)
	PoisonAtk  byte  // 怪物施毒能力（從模板載入）: 0=無, 1=傷害毒, 2=沉默毒, 4=麻痺毒
	FireRes    int16 // 火抗
	WaterRes   int16 // 水抗
	WindRes    int16 // 風抗
	EarthRes   int16 // 地抗

	// Spawn data for respawning
	SpawnX       int32
	SpawnY       int32
	SpawnMapID   int16
	RespawnDelay int   // seconds
	MobGroupID   int32 // 群體 ID（隊長記憶，重生時重新建立群體）

	// State
	Dead         bool
	DeleteTimer  int // ticks until S_RemoveObject is sent (Java: NPC_DELETION_TIME, default 10s = 50 ticks)
	RespawnTimer int // ticks remaining until respawn

	// AI state — 仇恨系統
	AggroTarget  uint64           // SessionID of hate target (0 = no target)，由仇恨列表驅動
	HateList     map[uint64]int32 // 仇恨列表 — key=SessionID, value=累積傷害仇恨值
	AttackTimer  int    // ticks until next attack (cooldown)
	MoveTimer    int    // ticks until next move towards target
	StuckTicks   int    // consecutive ticks blocked by another entity (for stuck detection)

	// Idle wandering state (Java: _randomMoveDistance / _randomMoveDirection)
	WanderDist   int   // remaining tiles to walk in current wander direction
	WanderDir    int16 // current wander heading (0-7)
	WanderTimer  int   // ticks until next wander step

	// 負面狀態（debuff）
	Paralyzed     bool           // 麻痺/凍結/暈眩 — 跳過所有 AI 行為
	Sleeped       bool           // 睡眠 — 跳過所有 AI 行為，受傷時解除
	ActiveDebuffs map[int32]int  // skillID → 剩餘 ticks（NPC 不需 stat delta，只需計時）

	// 家具系統
	FurnitureItemObjID int32 // 對應的道具 objectID（0 = 非家具 NPC）

	// 法術中毒系統（Java L1DamagePoison 對 NPC）
	PoisonDmgAmt      int32  // 每次扣血量（0=無毒）
	PoisonDmgTimer    int    // 距下次扣血的 tick 計數（每 15 tick 扣一次）
	PoisonAttackerSID uint64 // 施毒者 SessionID（仇恨歸屬用）

	// NPC 聊天計時器（Java: L1NpcChatTimer）
	ChatTiming        int    // 目前啟用的聊天時機（0=出現, 1=死亡, 4=被攻擊）
	ChatDelayTicks    int    // 首次延遲剩餘 ticks（0=已開始）
	ChatStep          int    // 目前播放到第幾個 chatId (0-based)
	ChatIntervalTicks int    // 對話間隔剩餘 ticks
	ChatRepeatTicks   int    // 重複間隔剩餘 ticks
	ChatActive        bool   // 聊天計時器是否啟用
	ChatFirstAttack   bool   // 是否已觸發過首次被攻擊聊天
	ChatAppearStarted bool   // 是否已嘗試過出現聊天（防重複啟動）

	// 怪物群體系統（Java: L1MobGroupInfo）
	GroupInfo    *MobGroupInfo // 所屬群體資訊（nil=不屬於任何群體）
	IsMinion     bool          // true=隊員（不獨立重生）
}

// MobGroupInfo 怪物群體運行時狀態。
// Java: L1MobGroupInfo — 記錄群體成員、隊長、解散規則。
type MobGroupInfo struct {
	Leader               *NpcInfo   // 隊長 NPC
	Members              []*NpcInfo // 所有成員（含隊長）
	RemoveGroupOnDeath   bool       // 隊長死亡時是否解散群體
}

// HasDebuff 檢查 NPC 是否有指定 debuff。
func (n *NpcInfo) HasDebuff(skillID int32) bool {
	if n.ActiveDebuffs == nil {
		return false
	}
	_, ok := n.ActiveDebuffs[skillID]
	return ok
}

// AddDebuff 對 NPC 施加 debuff（skillID → ticks）。
func (n *NpcInfo) AddDebuff(skillID int32, ticks int) {
	if n.ActiveDebuffs == nil {
		n.ActiveDebuffs = make(map[int32]int)
	}
	n.ActiveDebuffs[skillID] = ticks
}

// RemoveDebuff 移除 NPC 的指定 debuff。
func (n *NpcInfo) RemoveDebuff(skillID int32) {
	if n.ActiveDebuffs != nil {
		delete(n.ActiveDebuffs, skillID)
	}
}
