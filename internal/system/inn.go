package system

import (
	"context"
	"fmt"
	"time"

	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

const (
	innKeyItemID    int32         = 40312
	innPricePerKey  int32         = 300
	innRentalDurSys               = 4 * time.Hour
)

// InnSystem 處理旅館租房/退租邏輯。
// 實作 handler.InnManager 介面。
type InnSystem struct {
	deps *handler.Deps
}

// NewInnSystem 建立旅館系統。
func NewInnSystem(deps *handler.Deps) *InnSystem {
	return &InnSystem{deps: deps}
}

// ReturnRoom 處理退租：移除鑰匙、退還金幣、更新房間。
func (s *InnSystem) ReturnRoom(sess *net.Session, player *world.PlayerInfo, npcObjID, npcID int32) {
	rooms := s.deps.InnRooms[npcID]
	if rooms == nil {
		return
	}

	now := time.Now()
	price := int32(0)
	found := false

	for i := int32(0); i < 16; i++ {
		room := rooms[i]
		if room == nil || room.LodgerID != player.CharID {
			continue
		}
		if now.Before(room.DueTime) {
			price += 60
		}
		room.DueTime = time.Now()
		room.LodgerID = 0
		room.KeyID = 0
		room.Hall = false
		if s.deps.InnRepo != nil {
			_ = s.deps.InnRepo.UpdateRoom(context.Background(), room)
		}
		found = true
		break
	}

	// 移除鑰匙
	keysToRemove := []*world.InvItem{}
	for _, item := range player.Inv.Items {
		if item.InnNpcID == npcID {
			keysToRemove = append(keysToRemove, item)
		}
	}
	for _, key := range keysToRemove {
		price += 20 * key.Count
		player.Inv.RemoveItem(key.ObjectID, key.Count)
		handler.SendRemoveInventoryItem(sess, key.ObjectID)
		found = true
	}

	if found {
		if price > 0 {
			adena := player.Inv.FindByItemID(world.AdenaItemID)
			if adena != nil {
				adena.Count += price
				handler.SendItemCountUpdate(sess, adena)
			} else {
				adena = player.Inv.AddItem(world.AdenaItemID, price, "金幣", 5, 0, true, 0)
				handler.SendAddItem(sess, adena, nil)
			}
		}
		player.Dirty = true

		npc := s.deps.World.GetNpc(npcObjID)
		npcName := ""
		if npc != nil {
			npcName = npc.Name
		}
		handler.SendHypertextWithData(sess, npcObjID, "inn20", []string{
			npcName, fmt.Sprintf("%d", price),
		})
	} else {
		handler.SendHypertext(sess, npcObjID, "")
	}
}

// RentRoom 處理租房：扣金幣、建立鑰匙、更新房間。
func (s *InnSystem) RentRoom(sess *net.Session, player *world.PlayerInfo, npcObjID, npcID int32, amount int32) {
	totalCost := innPricePerKey * amount

	adena := player.Inv.FindByItemID(world.AdenaItemID)
	if adena == nil || adena.Count < totalCost {
		npc := s.deps.World.GetNpc(npcObjID)
		npcName := ""
		if npc != nil {
			npcName = npc.Name
		}
		handler.SendHypertextWithData(sess, npcObjID, "inn3", []string{npcName})
		return
	}

	rooms := s.deps.InnRooms[npcID]
	if rooms == nil {
		return
	}
	room := rooms[player.PendingInnRoomNum]
	if room == nil {
		return
	}
	now := time.Now()
	if now.Before(room.DueTime) {
		handler.SendHypertext(sess, npcObjID, "")
		return
	}

	dueTime := now.Add(innRentalDurSys)
	dueUnix := dueTime.Unix()

	keyInfo := s.deps.Items.Get(innKeyItemID)
	if keyInfo == nil {
		s.deps.Log.Warn("旅館鑰匙物品模板不存在", zap.Int32("itemID", innKeyItemID))
		return
	}

	keyItem := player.Inv.AddItem(innKeyItemID, amount, keyInfo.Name, keyInfo.InvGfx, keyInfo.Weight, false, 0)
	keyItem.InnKeyID = keyItem.ObjectID
	keyItem.InnNpcID = npcID
	keyItem.InnHall = player.PendingInnHall
	keyItem.InnDueTime = dueUnix

	room.KeyID = keyItem.InnKeyID
	room.LodgerID = player.CharID
	room.Hall = player.PendingInnHall
	room.DueTime = dueTime
	if s.deps.InnRepo != nil {
		_ = s.deps.InnRepo.UpdateRoom(context.Background(), room)
	}

	adena.Count -= totalCost
	handler.SendItemCountUpdate(sess, adena)
	handler.SendAddItem(sess, keyItem, keyInfo)
	handler.SendWeightUpdate(sess, player)
	player.Dirty = true

	npc := s.deps.World.GetNpc(npcObjID)
	npcName := ""
	if npc != nil {
		npcName = npc.Name
	}
	keyName := keyInfo.Name
	if amount > 1 {
		keyName = fmt.Sprintf("%s (%d)", keyName, amount)
	}
	handler.SendServerMessageArgs(sess, 143, npcName, keyName)
	handler.SendHypertextWithData(sess, npcObjID, "inn4", []string{npcName})

	s.deps.Log.Info("旅館租房",
		zap.String("player", player.Name),
		zap.Int32("npcID", npcID),
		zap.Int32("roomNum", room.RoomNumber),
		zap.Bool("hall", player.PendingInnHall),
		zap.Int32("keys", amount),
		zap.Int32("cost", totalCost),
	)
}
