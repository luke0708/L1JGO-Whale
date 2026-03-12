package world

// EquipSlot identifies an equipment slot on a character.
type EquipSlot int

const (
	SlotNone    EquipSlot = 0
	SlotHelm    EquipSlot = 1
	SlotArmor   EquipSlot = 2
	SlotGlove   EquipSlot = 3
	SlotBoots   EquipSlot = 4
	SlotShield  EquipSlot = 5
	SlotCloak   EquipSlot = 6
	SlotRing1   EquipSlot = 7
	SlotRing2   EquipSlot = 8
	SlotAmulet  EquipSlot = 9
	SlotBelt    EquipSlot = 10
	SlotWeapon  EquipSlot = 11
	SlotEarring EquipSlot = 12
	SlotGuarder EquipSlot = 13
	SlotTShirt  EquipSlot = 14
	SlotRing3       EquipSlot = 15 // 第3戒指欄（Lv76 任務開通）
	SlotRing4       EquipSlot = 16 // 第4戒指欄（Lv81 任務開通）
	SlotRuneLeft    EquipSlot = 17 // 符石
	SlotRune2       EquipSlot = 18 // 第二符石
	SlotEarring2    EquipSlot = 19 // 第二耳環
	SlotPants       EquipSlot = 20 // 褲子
	SlotExpand1     EquipSlot = 21 // 擴展欄1
	SlotExpand2     EquipSlot = 22 // 擴展欄2
	SlotExpand3     EquipSlot = 23 // 擴展欄3
	SlotExpand4     EquipSlot = 24 // 擴展欄4
	SlotBadge       EquipSlot = 25 // 徽章
	SlotPauldron    EquipSlot = 26 // 盾甲
	SlotMax         EquipSlot = 27
)

// Equipment tracks what a player currently has equipped.
// Each slot holds a pointer to an InvItem (nil = empty).
type Equipment struct {
	Slots [SlotMax]*InvItem
}

// Get returns the item in a slot, or nil.
func (e *Equipment) Get(slot EquipSlot) *InvItem {
	if slot <= SlotNone || slot >= SlotMax {
		return nil
	}
	return e.Slots[slot]
}

// Set places an item in a slot (or nil to clear).
func (e *Equipment) Set(slot EquipSlot, item *InvItem) {
	if slot > SlotNone && slot < SlotMax {
		e.Slots[slot] = item
	}
}

// Weapon returns the currently equipped weapon, or nil.
func (e *Equipment) Weapon() *InvItem {
	return e.Slots[SlotWeapon]
}

// ArmorSlotFromType maps an armor type string (from YAML) to an EquipSlot.
func ArmorSlotFromType(armorType string) EquipSlot {
	switch armorType {
	case "helm":
		return SlotHelm
	case "armor":
		return SlotArmor
	case "T", "t_shirts":
		return SlotTShirt
	case "cloak":
		return SlotCloak
	case "glove":
		return SlotGlove
	case "boots":
		return SlotBoots
	case "shield":
		return SlotShield
	case "guarder":
		return SlotGuarder
	case "ring":
		return SlotRing1 // caller should check Ring1 vs Ring2
	case "amulet", "necklace":
		return SlotAmulet
	case "earring":
		return SlotEarring
	case "belt":
		return SlotBelt
	case "runeword_left":
		return SlotRuneLeft
	case "rune2":
		return SlotRune2
	case "earring2":
		return SlotEarring2
	case "pants":
		return SlotPants
	case "expand1":
		return SlotExpand1
	case "expand2":
		return SlotExpand2
	case "expand3":
		return SlotExpand3
	case "expand4":
		return SlotExpand4
	case "badge":
		return SlotBadge
	case "pauldron":
		return SlotPauldron
	default:
		return SlotNone
	}
}

// IsTwoHanded returns true for weapon types that use both hands (no shield).
func IsTwoHanded(weaponType string) bool {
	switch weaponType {
	case "tohandsword", "tohandblunt", "bow", "claw", "edoryu":
		return true
	}
	return false
}

// WeaponVisualID maps a weapon type string to the client's visual animation byte.
// This byte is sent in S_CHANGE_DESC (opcode 119) and S_PUT_OBJECT (opcode 87).
func WeaponVisualID(weaponType string) byte {
	switch weaponType {
	case "sword":
		return 4
	case "dagger":
		return 46
	case "tohandsword":
		return 50
	case "bow":
		return 20
	case "spear", "singlespear":
		return 24
	case "blunt", "tohandblunt":
		return 24
	case "staff":
		return 40
	case "claw":
		return 58
	case "edoryu":
		return 54
	default:
		return 0 // no weapon / fist
	}
}

// EquipStats holds the cumulative stat bonuses from all equipped items.
type EquipStats struct {
	AC        int
	HitMod    int
	DmgMod    int
	BowHitMod int
	BowDmgMod int
	AddStr    int
	AddDex    int
	AddCon    int
	AddInt    int
	AddWis    int
	AddCha    int
	AddHP     int
	AddMP     int
	AddHPR    int
	AddMPR    int
	AddSP     int
	MDef      int

	// 元素抗性
	DefFire  int
	DefWater int
	DefWind  int
	DefEarth int

	// 狀態抗性
	RegistStun    int
	RegistStone   int
	RegistSleep   int
	RegistFreeze  int
	RegistSustain int
	RegistBlind   int

	// 傷害減免
	DmgReduction int
}

// IsAccessorySlot returns true for slots where enchant level does NOT affect AC.
// Java: armor type 8-12 (amulet, ring, guarder/bracer, earring) are accessories.
func IsAccessorySlot(slot EquipSlot) bool {
	switch slot {
	case SlotAmulet, SlotRing1, SlotRing2, SlotRing3, SlotRing4,
		SlotGuarder, SlotEarring, SlotEarring2,
		SlotRuneLeft, SlotRune2,
		SlotBadge, SlotPauldron:
		return true
	}
	return false
}

// EquipClientIndex 將 Go EquipSlot 映射為 3.80C 客戶端裝備欄索引。
// 值來自 Java L1Inventory.toSlotPacket()（已修改版「新增欄位顯示 琮善」）。
// 注意：此映射與 S_EquipmentWindow 常數定義不同——以 toSlotPacket 為準。
func EquipClientIndex(slot EquipSlot) byte {
	switch slot {
	case SlotHelm:
		return 1 // Java: type 1 (頭盔) → idx = type = 1
	case SlotArmor:
		return 2 // EQUIPMENT_INDEX_ARMOR = 2
	case SlotTShirt:
		return 3 // EQUIPMENT_INDEX_T = 3
	case SlotCloak:
		return 4 // Java: type 4 (斗篷) → idx = 4
	case SlotGlove:
		return 6 // EQUIPMENT_INDEX_GLOVE = 6
	case SlotBoots:
		return 5 // EQUIPMENT_INDEX_BOOTS = 5
	case SlotShield:
		return 7 // Java: type 7 (盾牌) → idx = 7
	case SlotBelt:
		return 11 // EQUIPMENT_INDEX_BELT = 11
	case SlotWeapon:
		return 8 // EQUIPMENT_INDEX_WEAPON 原始預設 = 8
	case SlotAmulet:
		return 10 // Java: type 8 (項鍊) → idx = 10
	case SlotGuarder:
		return 7 // Java: type 13 (臂甲) → idx = 7，與盾牌共用欄位
	case SlotEarring:
		return 12 // Java: type 12 (耳環) → idx = 12
	case SlotRing1:
		return 18 // Java: EQUIPMENT_INDEX_RING1 = 18
	case SlotRing2:
		return 19 // Java: EQUIPMENT_INDEX_RING2 = 19
	case SlotRing3:
		return 20 // Java: EQUIPMENT_INDEX_RING3 = 20
	case SlotRing4:
		return 21 // Java: EQUIPMENT_INDEX_RING4 = 21
	case SlotRuneLeft:
		return 22 // 符石
	case SlotRune2:
		return 23 // 第二符石
	case SlotEarring2:
		return 24 // 第二耳環
	case SlotPants:
		return 25 // 褲子
	case SlotExpand1:
		return 26 // 擴展欄1
	case SlotExpand2:
		return 27 // 擴展欄2
	case SlotExpand3:
		return 28 // 擴展欄3
	case SlotExpand4:
		return 29 // 擴展欄4
	case SlotBadge:
		return 30 // 徽章
	case SlotPauldron:
		return 31 // 盾甲
	}
	return 0
}
