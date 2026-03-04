package handler

import (
	"sync/atomic"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// yesNoCounter 是 S_Message_YN 對話框的全域序號（供 trade、party、clan 等共用）。
var yesNoCounter atomic.Int32

// sendYesNoDialog 發送 S_Message_YN (opcode 219)。
// Java 格式：[H 0x0000][D yesNoCount][H messageType][S param...]
func sendYesNoDialog(sess *net.Session, msgType uint16, args ...string) {
	count := yesNoCounter.Add(1)
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_YES_NO)
	w.WriteH(0)
	w.WriteD(count)
	w.WriteH(msgType)
	for _, arg := range args {
		w.WriteS(arg)
	}
	sess.Send(w.Bytes())
}

// SendYesNoDialog 匯出 sendYesNoDialog — 供 system 套件發送 Yes/No 確認對話框。
func SendYesNoDialog(sess *net.Session, msgType uint16, args ...string) {
	sendYesNoDialog(sess, msgType, args...)
}

// HandleAskTrade 處理 C_ASK_XCHG (opcode 2) — 發起交易請求。
// 解析封包 → 找面對面目標 → 委派給 TradeSystem。
func HandleAskTrade(sess *net.Session, _ *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil || player.Dead {
		return
	}

	// 已在交易中
	if player.TradePartnerID != 0 {
		return
	}

	// 尋找面對面的交易對象
	target := findFaceToFace(player, deps)
	if target == nil {
		SendGlobalChat(sess, 9, "找不到交易對象。")
		return
	}

	if deps.Trade != nil {
		deps.Trade.InitiateTrade(sess, player, target)
	}
}

// handleTradeYesNo 處理目標的 Yes/No 交易確認回應。
// 由 HandleAttr (attr.go) 和 NPC 動作呼叫。
func handleTradeYesNo(sess *net.Session, player *world.PlayerInfo, partnerID int32, accepted bool, deps *Deps) {
	if deps.Trade != nil {
		deps.Trade.HandleYesNo(sess, player, partnerID, accepted)
	}
}

// HandleAddTrade 處理 C_ADD_XCHG (opcode 37) — 加入交易物品。
// 格式：[D objectID][D count]
func HandleAddTrade(sess *net.Session, r *packet.Reader, deps *Deps) {
	objectID := r.ReadD()
	count := r.ReadD()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Trade != nil {
		deps.Trade.AddItem(sess, player, objectID, count)
	}
}

// HandleAcceptTrade 處理 C_ACCEPT_XCHG (opcode 71) — 確認交易。
func HandleAcceptTrade(sess *net.Session, _ *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	// NPC 製作交易模式：PendingCraftKey 有值但不在玩家交易中
	if player.PendingCraftKey != "" && player.TradePartnerID == 0 {
		handleCraftTradeConfirm(sess, player, deps)
		return
	}

	if deps.Trade != nil {
		deps.Trade.Accept(sess, player)
	}
}

// HandleCancelTrade 處理 C_CANCEL_XCHG (opcode 86) — 取消交易。
func HandleCancelTrade(sess *net.Session, _ *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	// NPC 製作交易模式：關閉交易視窗但保留配方狀態
	// ItemBlend 對話框仍在後方，玩家可再次點擊「製作道具」重開交易視窗
	if player.PendingCraftKey != "" && player.TradePartnerID == 0 {
		player.CraftTradeTick = 0
		sendTradeStatus(sess, 1)
		return
	}

	if player.TradePartnerID == 0 {
		return
	}

	if deps.Trade != nil {
		deps.Trade.Cancel(player)
	}
}

// CancelTradeIfActive 若玩家正在交易中則取消。Exported for system package usage.
func CancelTradeIfActive(player *world.PlayerInfo, deps *Deps) {
	cancelTradeIfActive(player, deps)
}

// cancelTradeIfActive 若玩家正在交易中則取消。
// 傳送、移動、開商店等各處呼叫此函式。
func cancelTradeIfActive(player *world.PlayerInfo, deps *Deps) {
	if player.TradePartnerID == 0 {
		return
	}
	if deps.Trade != nil {
		deps.Trade.CancelIfActive(player)
	}
}

// --- NPC 製作交易 ---

// handleCraftTradeConfirm 處理 NPC 製作的交易確認。
// 玩家在交易視窗按確認 → 關閉交易視窗 → 執行製作。
func handleCraftTradeConfirm(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	craftKey := player.PendingCraftKey
	npcID := player.PendingCraftNpcID
	player.PendingCraftKey = ""
	player.PendingCraftNpcID = 0
	player.CraftTradeTick = 0

	// 先關閉交易視窗
	sendTradeStatus(sess, 0)

	if deps.ItemMaking == nil || deps.Craft == nil {
		return
	}

	recipe := deps.ItemMaking.GetByNpcAction(npcID, craftKey)
	if recipe == nil {
		return
	}

	// 執行製作（npc=nil，因為交易視窗中無法取得 NPC 實例；
	// ExecuteCraft 已處理 npc=nil 的情況）
	deps.Craft.ExecuteCraft(sess, player, nil, recipe, 1)
}

// --- 交易封包函式 ---

// sendTradeOpen 發送 S_TRADE (opcode 52) — 開啟交易視窗。
// Java S_Trade: writeC(opcode) + writeS(name)，無其他欄位。
func sendTradeOpen(sess *net.Session, partnerName string) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_TRADE)
	w.WriteS(partnerName)
	sess.Send(w.Bytes())
}

// SendTradeOpen 匯出 sendTradeOpen。
func SendTradeOpen(sess *net.Session, partnerName string) {
	sendTradeOpen(sess, partnerName)
}

// sendTradeAddItem 發送 S_TRADEADDITEM (opcode 35) — 交易物品加入。
// panelType: 0=玩家側（下方）, 1=對方側（上方）
// Java S_TradeAddItem: writeC(opcode) + writeC(type) + writeH(gfxId) + writeS(name) + writeC(bless)
func sendTradeAddItem(sess *net.Session, gfxID uint16, viewName string, bless byte, panelType byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_TRADEADDITEM)
	w.WriteC(panelType)
	w.WriteH(gfxID)
	w.WriteS(viewName)
	w.WriteC(bless)
	sess.Send(w.Bytes())
}

// SendTradeAddItem 匯出 sendTradeAddItem。
func SendTradeAddItem(sess *net.Session, gfxID uint16, viewName string, bless byte, panelType byte) {
	sendTradeAddItem(sess, gfxID, viewName, bless, panelType)
}

// sendTradeStatus 發送 S_TRADESTATUS (opcode 112) — 交易狀態更新。
// 0=交易完成, 1=交易取消
func sendTradeStatus(sess *net.Session, status byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_TRADESTATUS)
	w.WriteC(status)
	sess.Send(w.Bytes())
}

// SendTradeStatus 匯出 sendTradeStatus。
func SendTradeStatus(sess *net.Session, status byte) {
	sendTradeStatus(sess, status)
}

// findFaceToFace 尋找面對面的玩家（相鄰格、反向朝向）。
// Java: L1World.findFaceToFace。
func findFaceToFace(player *world.PlayerInfo, deps *Deps) *world.PlayerInfo {
	h := player.Heading
	if h < 0 || h > 7 {
		return nil
	}

	targetX := player.X + headingDX[h]
	targetY := player.Y + headingDY[h]
	oppositeH := (h + 4) % 8

	nearby := deps.World.GetNearbyPlayers(player.X, player.Y, player.MapID, player.SessionID)
	for _, other := range nearby {
		if other.X == targetX && other.Y == targetY && other.Heading == oppositeH {
			return other
		}
	}
	return nil
}
