package handler

import (
	"math"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
)

// HandleFixWeaponList processes C_FIXABLE_ITEM (opcode 254) — player opens weapon repair NPC.
// The client sends an empty packet (just the opcode). Server responds with S_FixWeaponList (83).
// Java: C_FixWeaponList.java → sends new S_FixWeaponList(pc)
func HandleFixWeaponList(sess *net.Session, _ *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Inv == nil {
		return
	}

	// Collect all damaged weapons in inventory
	type damagedWeapon struct {
		objectID   int32
		durability int8
	}
	var weapons []damagedWeapon

	for _, item := range player.Inv.Items {
		if item.Durability <= 0 {
			continue
		}
		// Check if this item is a weapon (category == 1)
		itemInfo := deps.Items.Get(item.ItemID)
		if itemInfo == nil || itemInfo.Category != data.CategoryWeapon {
			continue
		}
		weapons = append(weapons, damagedWeapon{
			objectID:   item.ObjectID,
			durability: item.Durability,
		})
	}

	if len(weapons) == 0 {
		return // no damaged weapons to repair
	}

	// Build S_FixWeaponList (opcode 83)
	// Java format: [C opcode][D costPerPoint][H count]{[D objID][C durability]}
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_SELECTLIST)
	w.WriteD(int32(deps.Config.Gameplay.RepairCostPerDurability))
	w.WriteH(uint16(len(weapons)))
	for _, wpn := range weapons {
		w.WriteD(wpn.objectID)
		w.WriteC(byte(wpn.durability))
	}
	sess.Send(w.Bytes())
}

// HandleSelectList processes C_PERSONAL_SHOP (opcode 20) — weapon repair selection.
// Java format: [D itemObjectId][D npcObjectId]
// When npcObjectId != 0, this is a weapon repair request (not personal shop).
// Java: C_SelectList.java
func HandleSelectList(sess *net.Session, r *packet.Reader, deps *Deps) {
	itemObjID := r.ReadD()
	npcObjID := r.ReadD()

	// npcObjID == 0 means personal shop (not implemented)
	if npcObjID == 0 {
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Inv == nil {
		return
	}

	// Validate NPC exists and is within range (Chebyshev <= 3)
	npc := deps.World.GetNpc(npcObjID)
	if npc == nil {
		return
	}
	dx := int32(math.Abs(float64(player.X - npc.X)))
	dy := int32(math.Abs(float64(player.Y - npc.Y)))
	if dx > 3 || dy > 3 {
		return
	}

	// Find the weapon in inventory
	item := player.Inv.FindByObjectID(itemObjID)
	if item == nil || item.Durability <= 0 {
		return // not found or already perfect
	}

	// Verify it's a weapon
	itemInfo := deps.Items.Get(item.ItemID)
	if itemInfo == nil || itemInfo.Category != data.CategoryWeapon {
		return
	}

	// Calculate repair cost
	cost := int32(item.Durability) * int32(deps.Config.Gameplay.RepairCostPerDurability)
	if !deps.NpcSvc.RepairWeapon(sess, player, item, cost) {
		return // insufficient adena (silent fail, matching Java behavior)
	}
}

