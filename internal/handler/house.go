package handler

import (
	"context"
	"strings"

	"github.com/l1jgo/server/internal/data"
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// --- 管家 NPC (L1HousekeeperInstance) 對話與動作處理 ---
// Java: L1HousekeeperInstance.java, C_NPCAction.java

// handleHousekeeperTalk 處理管家 NPC 的對話（玩家點擊管家）。
// Java: L1HousekeeperInstance.onTalkAction()
func handleHousekeeperTalk(sess *net.Session, player *world.PlayerInfo, npcObjID int32, npcID int32, deps *Deps) {
	if deps.Houses == nil {
		return
	}

	// 透過管家 NPC ID 找到對應的住宅
	houseLoc := deps.Houses.GetByKeeper(npcID)
	if houseLoc == nil {
		return
	}
	houseID := houseLoc.HouseID

	// 檢查玩家的血盟是否擁有此小屋
	isOwner := false
	if player.ClanID != 0 {
		clan := deps.World.Clans.GetClan(player.ClanID)
		if clan != nil && clan.HasHouse == houseID {
			isOwner = true
		}
	}

	if isOwner {
		// 所有者 — 使用正常 NPC 對話（走 npc_action_list.yaml）
		action := deps.NpcActions.Get(npcID)
		if action != nil {
			htmlID := action.NormalAction
			if player.Lawful < -1000 && action.CaoticAction != "" {
				htmlID = action.CaoticAction
			}
			if htmlID != "" {
				sendHypertext(sess, npcObjID, htmlID)
				return
			}
		}
		return
	}

	// 非所有者 — 檢查此小屋是否有主人
	ownerClan := deps.World.Clans.FindClanByHouse(houseID)
	if ownerClan != nil {
		// 有主人 — 顯示 "agname" (血盟名/盟主名/小屋名)
		houseName := getHouseName(houseID, deps)
		sendHypertextWithData(sess, npcObjID, "agname", []string{
			ownerClan.ClanName,
			ownerClan.LeaderName,
			houseName,
		})
	} else {
		// 無主人（競售中）— 顯示 "agnoname" (小屋名)
		houseName := getHouseName(houseID, deps)
		sendHypertextWithData(sess, npcObjID, "agnoname", []string{houseName})
	}
}

// handleHousekeeperAction 處理管家 NPC 動作按鈕。
// Java: C_NPCAction.java 中 name/tel0-3/upgrade/hall/agsell 等分支。
func handleHousekeeperAction(sess *net.Session, player *world.PlayerInfo, npcObjID int32, npcID int32, action string, deps *Deps) bool {
	if deps.Houses == nil {
		return false
	}

	switch action {
	case "name":
		return handleHouseName(sess, player, npcObjID, npcID, deps)
	case "tel0", "tel1", "tel2", "tel3":
		return handleHouseTeleport(sess, player, npcID, action, deps)
	case "upgrade":
		return handleHouseUpgrade(sess, player, npcObjID, npcID, deps)
	case "hall":
		return handleHouseHall(sess, player, npcID, deps)
	default:
		// "agsell" 售屋動作由 S_SellHouse 處理（與拍賣系統整合，暫不實作）
		return false
	}
}

// handleHouseName 處理「改名」動作。
// Java: C_NPCAction "name" → S_Message_YN(512)
func handleHouseName(sess *net.Session, player *world.PlayerInfo, npcObjID int32, npcID int32, deps *Deps) bool {
	if player.ClanID == 0 {
		return true
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.HasHouse == 0 {
		return true
	}
	houseLoc := deps.Houses.GetByKeeper(npcID)
	if houseLoc == nil || houseLoc.HouseID != clan.HasHouse {
		return true // 不是自己小屋的管家
	}

	// 儲存 houseID 到 PendingYesNoData，發送改名確認
	player.PendingYesNoType = 512
	player.PendingYesNoData = clan.HasHouse
	sendYesNoDialog(sess, 512)
	return true
}

// handleHouseTeleport 處理「傳送到城鎮指定點」動作。
// Java: C_NPCAction "tel0"~"tel3" → L1HouseLocation.getHouseTeleportLoc()
func handleHouseTeleport(sess *net.Session, player *world.PlayerInfo, npcID int32, action string, deps *Deps) bool {
	if player.ClanID == 0 {
		return true
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.HasHouse == 0 {
		return true
	}
	houseLoc := deps.Houses.GetByKeeper(npcID)
	if houseLoc == nil || houseLoc.HouseID != clan.HasHouse {
		return true
	}

	// 解析 tel 編號
	var telNum int
	switch action {
	case "tel0":
		telNum = 0
	case "tel1":
		telNum = 1
	case "tel2":
		telNum = 2
	case "tel3":
		telNum = 3
	}

	x, y, mapID := getHouseTeleportLoc(clan.HasHouse, telNum)
	if x == 0 && y == 0 {
		return true
	}

	teleportPlayer(sess, player, x, y, mapID, 5, deps)
	return true
}

// handleHouseUpgrade 處理「購買地下盟屋」動作。
// Java: C_NPCAction "upgrade" — 500 萬金幣購買地下盟屋
func handleHouseUpgrade(sess *net.Session, player *world.PlayerInfo, npcObjID int32, npcID int32, deps *Deps) bool {
	if player.ClanID == 0 {
		return true
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.HasHouse == 0 {
		return true
	}
	houseLoc := deps.Houses.GetByKeeper(npcID)
	if houseLoc == nil || houseLoc.HouseID != clan.HasHouse {
		return true
	}

	// 古魯丁小屋（keeper 50626-50631）無地下室
	if npcID >= 50626 && npcID <= 50631 {
		SendServerMessage(sess, 189) // "金幣不足"（Java 用自訂訊息，這裡暫用通用訊息）
		return true
	}

	// 必須是王族盟主
	if player.ClassType != 0 || player.CharID != clan.LeaderID {
		SendServerMessage(sess, 518) // "只有血盟的君主才可以使用此指令"
		return true
	}

	// 檢查是否已購買
	if houseLoc.BasementMapID == 0 {
		// YAML 中無地下室地圖 — 不支援
		SendServerMessage(sess, 189)
		return true
	}

	// 檢查 DB 狀態 — 需透過 HouseRepo 讀取
	if deps.HouseRepo == nil {
		return true
	}
	states, err := deps.HouseRepo.LoadAll(context.Background())
	if err != nil {
		deps.Log.Error("載入住宅狀態失敗", zap.Error(err))
		return true
	}
	for _, s := range states {
		if s.HouseID == clan.HasHouse {
			if s.IsPurchaseBasement {
				SendServerMessage(sess, 1135) // "已購買地下盟屋"
				return true
			}
			break
		}
	}

	// 扣除 500 萬金幣
	const upgradeCost int32 = 5_000_000
	currentGold := player.Inv.GetAdena()
	if currentGold < upgradeCost {
		SendServerMessage(sess, 189) // "金幣不足"
		return true
	}

	// 先更新 DB
	if err := deps.HouseRepo.UpdateBasement(context.Background(), clan.HasHouse, true); err != nil {
		deps.Log.Error("更新地下盟屋失敗", zap.Error(err))
		return true
	}

	// DB 成功後扣金幣
	deps.NpcSvc.ConsumeAdena(sess, player, upgradeCost)

	SendServerMessage(sess, 1099) // "成功購買地下盟屋"
	return true
}

// handleHouseHall 處理「進入地下盟屋」動作。
// Java: C_NPCAction "hall" + instanceof L1HousekeeperInstance
func handleHouseHall(sess *net.Session, player *world.PlayerInfo, npcID int32, deps *Deps) bool {
	if player.ClanID == 0 {
		return true
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.HasHouse == 0 {
		return true
	}
	houseLoc := deps.Houses.GetByKeeper(npcID)
	if houseLoc == nil || houseLoc.HouseID != clan.HasHouse {
		return true
	}

	if houseLoc.BasementMapID == 0 {
		SendServerMessage(sess, 1098) // "尚未購買地下盟屋"
		return true
	}

	// 檢查是否已購買地下盟屋
	if deps.HouseRepo == nil {
		return true
	}
	states, err := deps.HouseRepo.LoadAll(context.Background())
	if err != nil {
		return true
	}
	purchased := false
	for _, s := range states {
		if s.HouseID == clan.HasHouse {
			purchased = s.IsPurchaseBasement
			break
		}
	}

	if !purchased {
		SendServerMessage(sess, 1098) // "尚未購買地下盟屋"
		return true
	}

	// 計算地下盟屋入口座標
	x, y, mapID := getBasementLoc(clan.HasHouse, houseLoc)
	teleportPlayer(sess, player, x, y, mapID, 5, deps)
	return true
}

// HandleHouseRename 處理改名確認回呼（S_Message_YN 512 的 yes 回應）。
// 由 C_Attr / handleYesNoResponse 呼叫。
func HandleHouseRename(sess *net.Session, player *world.PlayerInfo, houseID int32, newName string, deps *Deps) {
	if deps.HouseRepo == nil {
		return
	}
	newName = strings.TrimSpace(newName)
	if newName == "" || len(newName) > 32 {
		return
	}

	if err := deps.HouseRepo.UpdateName(context.Background(), houseID, newName); err != nil {
		deps.Log.Error("更新住宅名稱失敗", zap.Error(err))
	}
}

// --- 住宅傳送座標表 ---
// Java: L1HouseLocation.java

// 各城鎮傳送目的地 [4]：倉庫、寵物保管所、贖罪使者、吉蘭市場
// 地圖 ID 統一為 {4, 4, 4, 350}
var houseTeleportMapIDs = [4]int16{4, 4, 4, 350}

var houseTeleportGiran = [4][2]int32{
	{33419, 32810}, // 倉庫
	{33343, 32723}, // 寵物保管所
	{33553, 32712}, // 贖罪使者
	{32702, 32842}, // 吉蘭市場
}

var houseTeleportHeine = [4][2]int32{
	{33604, 33236}, // 倉庫
	{33649, 33413}, // 寵物保管所
	{33553, 32712}, // 贖罪使者
	{32702, 32842}, // 吉蘭市場
}

var houseTeleportAden = [4][2]int32{
	{33966, 33253}, // 倉庫
	{33921, 33177}, // 寵物保管所
	{33553, 32712}, // 贖罪使者
	{32702, 32842}, // 吉蘭市場
}

var houseTeleportGludin = [4][2]int32{
	{32628, 32807}, // 倉庫
	{32623, 32729}, // 寵物保管所
	{33553, 32712}, // 贖罪使者
	{32702, 32842}, // 吉蘭市場
}

// getHouseTeleportLoc 依住宅 ID 和傳送編號回傳目的座標。
// Java: L1HouseLocation.getHouseTeleportLoc()
func getHouseTeleportLoc(houseID int32, number int) (x, y int32, mapID int16) {
	if number < 0 || number > 3 {
		return 0, 0, 0
	}
	var locs *[4][2]int32
	switch {
	case houseID >= 262145 && houseID <= 262189: // 奇岩
		locs = &houseTeleportGiran
	case houseID >= 327681 && houseID <= 327691: // 海音
		locs = &houseTeleportHeine
	case houseID >= 458753 && houseID <= 458819: // 亞丁
		locs = &houseTeleportAden
	case houseID >= 524289 && houseID <= 524294: // 古魯丁
		locs = &houseTeleportGludin
	default:
		return 0, 0, 0
	}
	return locs[number][0], locs[number][1], houseTeleportMapIDs[number]
}

// getBasementLoc 計算地下盟屋入口座標。
// Java: L1HouseLocation.getBasementLoc()
func getBasementLoc(houseID int32, loc *data.HouseLocation) (x, y int32, mapID int16) {
	switch {
	case houseID >= 262145 && houseID <= 262189: // 奇岩
		return 32766, 32832, int16(houseID - 257077)
	case houseID >= 327681 && houseID <= 327691: // 海音
		return 32766, 32829, int16(houseID - 322568)
	case houseID >= 524289 && houseID <= 524294: // 古魯丁（無地下室，回傳小屋入口）
		return loc.HomeX, loc.HomeY, loc.MapID
	default:
		return loc.HomeX, loc.HomeY, loc.MapID
	}
}

// getHouseName 取得住宅名稱（優先從 DB，否則用預設名稱）。
func getHouseName(houseID int32, deps *Deps) string {
	if deps.HouseRepo != nil {
		states, err := deps.HouseRepo.LoadAll(context.Background())
		if err == nil {
			for _, s := range states {
				if s.HouseID == houseID {
					return s.HouseName
				}
			}
		}
	}
	return "血盟小屋"
}

