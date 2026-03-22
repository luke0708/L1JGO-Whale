package data

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolymorphInfo holds a single polymorph form definition.
type PolymorphInfo struct {
	PolyID      int32  // GFX sprite ID (also used as lookup key)
	Name        string // monster name for monlist lookup
	MinLevel    int    // minimum player level required
	WeaponEquip int    // weapon bitmask (0 = all weapons forbidden)
	ArmorEquip  int    // armor bitmask (0 = all armor forbidden)
	CanUseSkill bool   // false = cannot cast spells while polymorphed
	Cause       int    // trigger bitmask: 1=magic, 2=GM, 4=NPC, 8=keplisha
}

// Weapon equip bitmask constants (Java: L1PolyMorph.weaponFlgMap)
const (
	PolyWeaponDagger      = 1
	PolyWeaponSword       = 2
	PolyWeaponTwoHandSword = 4
	PolyWeaponAxe         = 8
	PolyWeaponSpear       = 16
	PolyWeaponStaff       = 32
	PolyWeaponEdoryu      = 64
	PolyWeaponClaw        = 128
	PolyWeaponBow         = 256
	PolyWeaponKiringku    = 512
	PolyWeaponChainSword  = 1024
)

// weaponTypeToFlag maps item type string to weapon equip bitmask flag.
var weaponTypeToFlag = map[string]int{
	"dagger":         PolyWeaponDagger,
	"sword":          PolyWeaponSword,
	"two_hand_sword": PolyWeaponTwoHandSword,
	"axe":            PolyWeaponAxe,
	"spear":          PolyWeaponSpear,
	"staff":          PolyWeaponStaff,
	"edoryu":         PolyWeaponEdoryu,
	"claw":           PolyWeaponClaw,
	"bow":            PolyWeaponBow,
	"gauntlet":       PolyWeaponBow, // gauntlet shares bow flag
	"kiringku":       PolyWeaponKiringku,
	"chainsword":     PolyWeaponChainSword,
}

// Armor equip bitmask constants (Java: L1PolyMorph.armorFlgMap)
const (
	PolyArmorHelm    = 1
	PolyArmorArmor   = 2
	PolyArmorTShirt  = 4
	PolyArmorCloak   = 8
	PolyArmorGlove   = 16
	PolyArmorBoots   = 32
	PolyArmorShield  = 64
	PolyArmorAmulet  = 128
	PolyArmorRingL   = 256
	PolyArmorRingR   = 512
	PolyArmorBelt    = 1024
	PolyArmorGuarder = 2048
)

// armorTypeToFlag maps armor type string to armor equip bitmask flag.
var armorTypeToFlag = map[string]int{
	"helm":    PolyArmorHelm,
	"armor":   PolyArmorArmor,
	"T":       PolyArmorTShirt,
	"cloak":   PolyArmorCloak,
	"glove":   PolyArmorGlove,
	"boots":   PolyArmorBoots,
	"shield":  PolyArmorShield,
	"guarder": PolyArmorGuarder,
	"amulet":  PolyArmorAmulet,
	"ring":    PolyArmorRingL | PolyArmorRingR,
	"belt":    PolyArmorBelt,
}

// Polymorph cause bitmask constants
const (
	PolyCauseMagic = 1
	PolyCauseGM    = 2
	PolyCauseNPC   = 4
)

// IsWeaponEquipable returns true if the weapon type is allowed for this polymorph.
func (p *PolymorphInfo) IsWeaponEquipable(weaponType string) bool {
	flag, ok := weaponTypeToFlag[weaponType]
	if !ok {
		return false
	}
	return p.WeaponEquip&flag != 0
}

// IsArmorEquipable returns true if the armor type is allowed for this polymorph.
// 未在 bitmask 映射中的類型（如 earring）預設允許裝備。
func (p *PolymorphInfo) IsArmorEquipable(armorType string) bool {
	flag, ok := armorTypeToFlag[armorType]
	if !ok {
		return true // 未知類型預設允許（耳環等不受變身限制）
	}
	return p.ArmorEquip&flag != 0
}

// IsMatchCause returns true if the given cause is allowed for this polymorph.
// cause=0 means bypass (e.g. login restoration).
func (p *PolymorphInfo) IsMatchCause(cause int) bool {
	if cause == 0 {
		return true
	}
	return p.Cause&cause != 0
}

// PolymorphTable holds all polymorph forms indexed by poly_id and name.
type PolymorphTable struct {
	byID   map[int32]*PolymorphInfo
	byName map[string]*PolymorphInfo
}

// GetByID returns a polymorph form by GFX ID, or nil if not found.
func (t *PolymorphTable) GetByID(polyID int32) *PolymorphInfo {
	return t.byID[polyID]
}

// GetByName returns a polymorph form by name (case-insensitive), or nil if not found.
func (t *PolymorphTable) GetByName(name string) *PolymorphInfo {
	return t.byName[strings.ToLower(name)]
}

// Count returns total loaded polymorph forms.
func (t *PolymorphTable) Count() int {
	return len(t.byID)
}

// --- YAML loading ---

type polymorphEntry struct {
	PolyID      int32  `yaml:"poly_id"`
	Name        string `yaml:"name"`
	MinLevel    int    `yaml:"min_level"`
	WeaponEquip int    `yaml:"weapon_equip"`
	ArmorEquip  int    `yaml:"armor_equip"`
	CanUseSkill bool   `yaml:"can_use_skill"`
	Cause       int    `yaml:"cause"`
}

type polymorphListFile struct {
	Polymorphs []polymorphEntry `yaml:"polymorphs"`
}

// LoadPolymorphTable loads polymorph form definitions from YAML.
func LoadPolymorphTable(path string) (*PolymorphTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read polymorphs: %w", err)
	}
	var f polymorphListFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse polymorphs: %w", err)
	}
	t := &PolymorphTable{
		byID:   make(map[int32]*PolymorphInfo, len(f.Polymorphs)),
		byName: make(map[string]*PolymorphInfo, len(f.Polymorphs)),
	}
	for i := range f.Polymorphs {
		e := &f.Polymorphs[i]
		info := &PolymorphInfo{
			PolyID:      e.PolyID,
			Name:        e.Name,
			MinLevel:    e.MinLevel,
			WeaponEquip: e.WeaponEquip,
			ArmorEquip:  e.ArmorEquip,
			CanUseSkill: e.CanUseSkill,
			Cause:       e.Cause,
		}
		t.byID[e.PolyID] = info
		t.byName[strings.ToLower(e.Name)] = info
	}
	return t, nil
}
