package handler

import (
	"fmt"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// handlePowerItemList 顯示強化物品清單（NPC 販賣的預製強化物品）。
// Java 參考: S_PowerItemList.java
// 格式: opcode 176 + writeD(objID) + writeH(count) + writeC(type=12) +
//
//	per item: writeD(order) + writeC(0) + writeH(gfx) + writeC(bless) +
//	          writeD(1) + writeC(identified) + writeS(name)
func handlePowerItemList(sess *net.Session, npcID, objID int32, deps *Deps) {
	if deps.PowerItems == nil {
		return
	}

	items := deps.PowerItems.Get(npcID)
	if len(items) == 0 {
		return
	}

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_RETRIEVE_LIST) // opcode 176
	w.WriteD(objID)
	w.WriteH(uint16(len(items)))
	w.WriteC(12) // type = 12（非玩家版，顯示 NPC 物品）

	for i, pItem := range items {
		itemInfo := deps.Items.Get(pItem.ItemID)
		name := fmt.Sprintf("item#%d", pItem.ItemID)
		gfxID := int32(0)
		if itemInfo != nil {
			name = itemInfo.Name
			gfxID = itemInfo.InvGfx
		}

		// 強化等級前綴
		if pItem.EnchantLvl > 0 {
			name = fmt.Sprintf("+%d %s", pItem.EnchantLvl, name)
		}

		// 價格標示
		name = fmt.Sprintf("%s (%d 金幣)", name, pItem.Price)

		w.WriteD(int32(i + 1)) // 1-based 序號
		w.WriteC(0)            // 固定值
		w.WriteH(uint16(gfxID))
		w.WriteC(byte(pItem.Bless))
		w.WriteD(1) // 數量固定 1
		w.WriteC(1) // 已鑑定
		w.WriteS(name)
	}

	// 記住瀏覽的 NPC 以便購買時查詢
	player.PowerItemNpcID = npcID

	sess.Send(w.Bytes())
}

// handlePowerItemBuy 處理購買強化物品。
// 從 HandleBuySell 路由（resultType == 12 且 NPC Impl == "L1PowerItem"）。
// 封包格式：per item: readD(orderId) + readD(count)
func handlePowerItemBuy(sess *net.Session, r *packet.Reader, count int, player *world.PlayerInfo, npc *world.NpcInfo, deps *Deps) {
	if deps.PowerItems == nil {
		return
	}

	items := deps.PowerItems.Get(player.PowerItemNpcID)
	if len(items) == 0 {
		return
	}

	for i := 0; i < count; i++ {
		orderID := int(r.ReadD())
		qty := r.ReadD()

		// 強化物品只能購買 1 個
		if qty != 1 {
			continue
		}

		idx := orderID - 1
		if idx < 0 || idx >= len(items) {
			continue
		}

		pItem := items[idx]

		// 委派給系統執行購買
		deps.PowerItemMgr.BuyPowerItem(sess, player, pItem)
	}
}
