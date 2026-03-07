package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// --- 結婚系統 ---
// Java: C_Propose.java (opcode 50), C_Attr case 653/654
// 結婚戒指物品 ID: 40901-40908

// 結婚相關常數
const (
	// 教堂座標範圍（地圖 4）
	churchMinX int32 = 33974
	churchMaxX int32 = 33976
	churchMinY int32 = 33362
	churchMaxY int32 = 33365
	churchMap  int16 = 4

	// 結婚戒指物品 ID 範圍
	ringMinID int32 = 40901
	ringMaxID int32 = 40908
)

// HandleMarriage 處理 C_Propose（opcode 50）— 求婚/離婚。
// Java: C_Propose.java — readC() = mode (0=求婚, 1=離婚)
func HandleMarriage(sess *net.Session, r *packet.Reader, deps *Deps) {
	mode := r.ReadC()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	deps.Log.Debug("C_Propose",
		zap.String("player", player.Name),
		zap.Uint8("mode", mode),
	)

	switch mode {
	case 0:
		handlePropose(sess, player, deps)
	case 1:
		handleDivorceRequest(sess, player, deps)
	}
}

// handlePropose 處理求婚（mode=0）。
func handlePropose(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.PartnerID != 0 {
		SendServerMessage(sess, 658) // "你(妳)的對象已經結婚了"
		return
	}

	if !inChurch(player) {
		SendServerMessage(sess, 661) // 必須在教堂
		return
	}

	if !hasRing(player) {
		SendServerMessage(sess, 659) // "你(妳)沒有結婚戒指"
		return
	}

	target := findNearbyPlayer(player, deps)
	if target == nil {
		SendServerMessage(sess, 93) // "你注視的地方沒有人"
		return
	}

	if target.PartnerID != 0 {
		SendServerMessage(sess, 658)
		return
	}

	if int(player.Level)+int(target.Level) < 50 {
		SendServerMessage(sess, 661)
		return
	}

	if !inChurch(target) {
		return
	}

	if !hasRing(target) {
		SendServerMessage(sess, 660) // "你(妳)的對象沒有結婚戒指"
		return
	}

	// 發送求婚 Y/N
	target.PendingYesNoType = 654
	target.PendingYesNoData = player.CharID
	sendYesNoDialog(target.Session, 654, player.Name)
}

// handleDivorceRequest 處理離婚請求（mode=1）。
func handleDivorceRequest(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.PartnerID == 0 {
		SendServerMessage(sess, 662) // "你(妳)目前未婚"
		return
	}

	player.PendingYesNoType = 653
	player.PendingYesNoData = player.CharID
	sendYesNoDialog(sess, 653)
}

// HandleMarriageAccept 處理求婚接受回應（C_Attr mode=654）。委派給 MarriageSystem。
func HandleMarriageAccept(sess *net.Session, player *world.PlayerInfo, proposerID int32, accepted bool, deps *Deps) {
	if deps.Marriage != nil {
		deps.Marriage.AcceptProposal(sess, player, proposerID, accepted)
	}
}

// HandleDivorceConfirm 處理離婚確認回應（C_Attr mode=653）。委派給 MarriageSystem。
func HandleDivorceConfirm(sess *net.Session, player *world.PlayerInfo, accepted bool, deps *Deps) {
	if deps.Marriage != nil {
		deps.Marriage.ConfirmDivorce(sess, player, accepted)
	}
}

// FindRingID 查找玩家背包中的結婚戒指 ID。Exported for system package.
func FindRingID(p *world.PlayerInfo) int32 {
	return findRingID(p)
}

// --- 戒指傳送功能 ---

// HandleRingTeleport 處理使用結婚戒指傳送到配偶身邊。
func HandleRingTeleport(sess *net.Session, player *world.PlayerInfo, item *world.InvItem, deps *Deps) {
	if player.PartnerID == 0 {
		SendServerMessage(sess, 662)
		return
	}

	if player.MarriageRingID == 0 || item.ItemID != player.MarriageRingID {
		SendServerMessage(sess, 79) // "沒有任何事情發生"
		return
	}

	partner := deps.World.GetByCharID(player.PartnerID)
	if partner == nil {
		SendServerMessage(sess, 546) // 配偶不在線
		return
	}

	if partner.PartnerID != player.CharID {
		SendServerMessage(sess, 546)
		return
	}

	// TODO: 寶石類戒指充電檢查（40903-40908）— 需要 InvItem 新增 ChargeCount 欄位

	// 傳送到配偶位置
	TeleportPlayer(sess, player, partner.X, partner.Y, partner.MapID, 5, deps)
}

// IsMarriageRing 檢查物品是否為結婚戒指。
func IsMarriageRing(itemID int32) bool {
	return itemID >= ringMinID && itemID <= ringMaxID
}

// --- 輔助函式 ---

func inChurch(p *world.PlayerInfo) bool {
	return p.MapID == churchMap &&
		p.X >= churchMinX && p.X <= churchMaxX &&
		p.Y >= churchMinY && p.Y <= churchMaxY
}

func hasRing(p *world.PlayerInfo) bool {
	if p.Inv == nil {
		return false
	}
	for _, item := range p.Inv.Items {
		if item.ItemID >= ringMinID && item.ItemID <= ringMaxID {
			return true
		}
	}
	return false
}

func findRingID(p *world.PlayerInfo) int32 {
	if p.Inv == nil {
		return 0
	}
	for _, item := range p.Inv.Items {
		if item.ItemID >= ringMinID && item.ItemID <= ringMaxID {
			return item.ItemID
		}
	}
	return 0
}

func findNearbyPlayer(player *world.PlayerInfo, deps *Deps) *world.PlayerInfo {
	dx, dy := HeadingOffset(player.Heading)
	targetX := player.X + dx
	targetY := player.Y + dy

	nearby := deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, 0)
	for _, other := range nearby {
		if other.CharID != player.CharID && other.X == targetX && other.Y == targetY {
			return other
		}
	}
	return nil
}

// HeadingOffset 根據朝向回傳偏移量。Exported for system package usage.
func HeadingOffset(heading int16) (int32, int32) {
	switch heading {
	case 0:
		return 0, 1
	case 1:
		return -1, 1
	case 2:
		return -1, 0
	case 3:
		return -1, -1
	case 4:
		return 0, -1
	case 5:
		return 1, -1
	case 6:
		return 1, 0
	case 7:
		return 1, 1
	default:
		return 0, 1
	}
}

func sendMarriageRemoveItem(sess *net.Session, objectID int32) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_REMOVE_INVENTORY)
	w.WriteD(objectID)
	sess.Send(w.Bytes())
}

// TODO: sendMarriageChargeUpdate — 需要 InvItem 新增 ChargeCount 欄位後實作
