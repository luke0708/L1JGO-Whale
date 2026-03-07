package system

import (
	"github.com/l1jgo/server/internal/handler"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
)

// MarriageSystem 處理結婚/離婚邏輯。
// 實作 handler.MarriageManager 介面。
type MarriageSystem struct {
	deps *handler.Deps
}

// NewMarriageSystem 建立結婚系統。
func NewMarriageSystem(deps *handler.Deps) *MarriageSystem {
	return &MarriageSystem{deps: deps}
}

// AcceptProposal 處理求婚接受。
func (s *MarriageSystem) AcceptProposal(sess *net.Session, player *world.PlayerInfo, proposerID int32, accepted bool) {
	proposer := s.deps.World.GetByCharID(proposerID)

	if !accepted {
		if proposer != nil {
			handler.SendServerMessageStr(proposer.Session, 656, player.Name) // "%0 拒絕你(妳)的求婚"
		}
		return
	}

	if proposer == nil {
		return
	}

	playerRingID := handler.FindRingID(player)
	proposerRingID := handler.FindRingID(proposer)
	if playerRingID == 0 || proposerRingID == 0 {
		return
	}

	// 設定雙方結婚狀態
	player.PartnerID = proposer.CharID
	player.MarriageRingID = playerRingID
	proposer.PartnerID = player.CharID
	proposer.MarriageRingID = proposerRingID

	// 通知雙方
	handler.SendServerMessage(sess, 790)
	handler.SendServerMessageStr(sess, 655, proposer.Name)
	handler.SendServerMessage(proposer.Session, 790)
	handler.SendServerMessageStr(proposer.Session, 655, player.Name)

	player.Dirty = true
	proposer.Dirty = true
}

// ConfirmDivorce 處理離婚確認。
func (s *MarriageSystem) ConfirmDivorce(sess *net.Session, player *world.PlayerInfo, accepted bool) {
	if !accepted || player.PartnerID == 0 {
		return
	}

	// 處理配偶（線上）
	partner := s.deps.World.GetByCharID(player.PartnerID)
	if partner != nil {
		partner.PartnerID = 0
		partner.MarriageRingID = 0
		partner.Dirty = true
		handler.SendServerMessage(partner.Session, 662) // "你(妳)目前未婚"
	}

	// 移除發起人的結婚戒指
	if player.MarriageRingID != 0 && player.Inv != nil {
		for _, item := range player.Inv.Items {
			if item.ItemID == player.MarriageRingID {
				player.Inv.RemoveItem(item.ObjectID, 1)
				handler.SendRemoveInventoryItem(sess, item.ObjectID)
				break
			}
		}
	}

	player.PartnerID = 0
	player.MarriageRingID = 0
	player.Dirty = true
	handler.SendServerMessage(sess, 662) // "你(妳)目前未婚"
}
