package handler

import (
	"time"

	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// ==================== 封包處理器（薄層：解封包 → 委派 ClanSystem） ====================

// HandleCreateClan 處理 C_CREATE_PLEDGE (opcode 222) — 建立血盟。
// 封包：[S clanName]
func HandleCreateClan(sess *net.Session, r *packet.Reader, deps *Deps) {
	clanName := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.Create(sess, player, clanName)
	}
}

// HandleJoinClan 處理 C_JOIN_PLEDGE (opcode 194) — 申請加入血盟。
// 封包：（無額外資料）
func HandleJoinClan(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.JoinRequest(sess, player)
	}
}

// HandleClanJoinResponse 處理 C_Attr case 97（血盟加入 Y/N 回應）。
// 由 attr.go 呼叫。
func HandleClanJoinResponse(sess *net.Session, responder *world.PlayerInfo, applicantCharID int32, accepted bool, deps *Deps) {
	if deps.Clan != nil {
		deps.Clan.JoinResponse(sess, responder, applicantCharID, accepted)
	}
}

// HandleLeaveClan 處理 C_LEAVE_PLEDGE (opcode 61) — 離開或解散血盟。
// 封包：[S clanName]
func HandleLeaveClan(sess *net.Session, r *packet.Reader, deps *Deps) {
	clanNamePkt := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.Leave(sess, player, clanNamePkt)
	}
}

// HandleBanMember 處理 C_BAN_MEMBER (opcode 69) — 驅逐血盟成員。
// 封包：[S targetName]
func HandleBanMember(sess *net.Session, r *packet.Reader, deps *Deps) {
	targetName := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.BanMember(sess, player, targetName)
	}
}

// HandleWhoPledge 處理 C_WHO_PLEDGE (opcode 68) — 檢視血盟資訊。
// 封包：（無額外資料）
func HandleWhoPledge(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.ShowClanInfo(sess, player)
	}
}

// HandlePledgeWatch 處理 C_PLEDGE_WATCH (opcode 78) — 多用途封包。
// Java: C_PledgeContent — dataType 決定用途：
//   13 = 火神精煉（分解物品為結晶）
//   14 = 火神合成（材料合成裝備）
//   15 = 寫入血盟公告
//   16 = 寫入個人備註
func HandlePledgeWatch(sess *net.Session, r *packet.Reader, deps *Deps) {
	dataType := r.ReadC()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	switch dataType {
	case 13:
		// 火神精煉（type 48）— Java: C_PledgeContent case 13
		handleRefineResolve(sess, r, player, deps)
	case 14:
		// 火神合成（type 49）— Java: C_PledgeContent case 14
		handleRefineTransform(sess, r, player, deps)
	case 15, 16:
		// 血盟公告/個人備註 — 原有邏輯
		content := r.ReadS()
		if deps.Clan != nil {
			deps.Clan.UpdateSettings(sess, player, dataType, content)
		}
	}
}

// HandleRankControl 處理 C_RANK_CONTROL (opcode 63)。
// 封包：[C data][C giverank][S name]
// Java: C_Rank.java — 多用途封包：data=1 血盟階級, data=9 地圖計時器(Ctrl+Q) 等。
func HandleRankControl(sess *net.Session, r *packet.Reader, deps *Deps) {
	data := r.ReadC()
	giveRank := int16(r.ReadC())
	targetName := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	switch data {
	case 1:
		// 血盟階級變更
		if deps.Clan != nil {
			deps.Clan.ChangeRank(sess, player, giveRank, targetName)
		}
	case 5:
		// 生存吶喊（Java: C_Rank case 5）
		handleSurvivalShout(sess, player)
	case 9:
		// Ctrl+Q 查詢限時地圖剩餘時間
		// Java: pc.sendPackets(new S_PacketBoxMapTimer(pc))
		SendMapTimerOut(sess, player)
	}
}

// handleSurvivalShout 處理生存吶喊（Java: C_Rank case 5）。
// 前置條件：飽食度 225、裝備武器、距吃飽至少 1 分鐘。
// 效果：依飽食飽和持續時間回復 HP，然後飽食度歸零。
// 1-29 分鐘: (分鐘數/100) × MaxHP
// 30+ 分鐘: 依武器強化等級回復 20%-70%
func handleSurvivalShout(sess *net.Session, player *world.PlayerInfo) {
	// 條件 1：飽食度必須 225
	if player.Food < 225 {
		SendSystemMessage(sess, "飽食度不足，無法使用生存的吶喊。")
		return
	}

	// 條件 2：必須裝備武器
	if player.Equip.Weapon() == nil {
		SendSystemMessage(sess, "必須裝備武器才能使用生存的吶喊。")
		return
	}

	// 條件 3：距吃飽至少 1 分鐘
	if player.FoodFullTime <= 0 {
		// 1974：還無法使用生存的吶喊。
		SendServerMessage(sess, 1974)
		return
	}
	now := time.Now().Unix()
	minutes := int((now - player.FoodFullTime) / 60)
	if minutes <= 0 {
		SendServerMessage(sess, 1974)
		return
	}

	// 計算回復量
	addHP := int16(0)
	if minutes >= 30 {
		// 30 分鐘後：依武器強化等級
		enchant := int(player.Equip.Weapon().EnchantLvl)
		var pct float64
		switch {
		case enchant >= 11:
			pct = 0.70
		case enchant >= 9:
			pct = 0.55 // 9-10 平均
		case enchant >= 7:
			pct = 0.45 // 7-8 平均
		default:
			pct = 0.30 // 0-6 平均
		}
		addHP = int16(float64(player.MaxHP) * pct)
	} else {
		// 1-29 分鐘: (分鐘數/100) × MaxHP
		addHP = int16(float64(player.MaxHP) * float64(minutes) / 100.0)
	}

	if addHP <= 0 {
		SendServerMessage(sess, 1974)
		return
	}

	// 消耗飽食度
	player.Food = 0
	player.FoodFullTime = -1
	SendFoodUpdate(sess, player.Food)

	// 回復 HP
	player.HP += addHP
	if player.HP > player.MaxHP {
		player.HP = player.MaxHP
	}
	SendHpUpdate(sess, player)

	// 特效音效（Java: S_SkillSound(4013)）
	SendSkillEffect(sess, player.CharID, 4013)
}

// HandleTitle 處理 C_TITLE (opcode 90) — 設定稱號。
// 封包：[S charName][S title]
func HandleTitle(sess *net.Session, r *packet.Reader, deps *Deps) {
	charName := r.ReadS()
	title := r.ReadS()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	if deps.Clan != nil {
		deps.Clan.SetTitle(sess, player, charName, title)
	}
}

// HandleEmblemUpload 處理 C_UPLOAD_EMBLEM (opcode 18) — 上傳盟徽。
// 封包：[384 bytes 盟徽資料]
func HandleEmblemUpload(sess *net.Session, r *packet.Reader, deps *Deps) {
	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	emblemData := r.ReadBytes(384)

	if deps.Clan != nil {
		deps.Clan.UploadEmblem(sess, player, emblemData)
	}
}

// HandleEmblemDownload 處理 C_ALT_ATTACK / C_EMBLEM_DOWNLOAD (opcode 72) — 下載盟徽。
// 封包：[D emblemId]
func HandleEmblemDownload(sess *net.Session, r *packet.Reader, deps *Deps) {
	emblemID := r.ReadD()

	if deps.Clan != nil {
		deps.Clan.DownloadEmblem(sess, emblemID)
	}
}

// ==================== 封包建構（enterworld.go 等需要） ====================

// sendClanName 發送 S_OPCODE_CLANNAME (72) — 更新血盟名稱顯示。
// join=true → flag 0x0a (啟用), join=false → flag 0x0b (離開)
func sendClanName(sess *net.Session, objID int32, clanName string, clanID int32, join bool) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANNAME)
	w.WriteD(objID)
	w.WriteS(clanName)
	w.WriteD(0)
	w.WriteC(0)
	if join {
		w.WriteC(0x0a)
		w.WriteD(0)
	} else {
		w.WriteC(0x0b)
		w.WriteD(clanID)
	}
	sess.Send(w.Bytes())
}

// sendPledgeEmblemStatus 發送 S_PacketBox(173) — 盟徽狀態通知。
func sendPledgeEmblemStatus(sess *net.Session, emblemStatus int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteC(173)
	w.WriteC(1)
	if emblemStatus == 0 {
		w.WriteC(0)
	} else {
		w.WriteC(1)
	}
	w.WriteD(0)
	sess.Send(w.Bytes())
}

// sendClanAttention 發送 S_OPCODE_CLANATTENTION (200) — 血盟狀態通知。
func sendClanAttention(sess *net.Session) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANATTENTION)
	w.WriteD(2)
	sess.Send(w.Bytes())
}

// ==================== 匯出包裝器（供 system 套件使用） ====================

// SendClanName 匯出 sendClanName。
func SendClanName(sess *net.Session, objID int32, clanName string, clanID int32, join bool) {
	sendClanName(sess, objID, clanName, clanID, join)
}

// SendPledgeEmblemStatus 匯出 sendPledgeEmblemStatus。
func SendPledgeEmblemStatus(sess *net.Session, emblemStatus int) {
	sendPledgeEmblemStatus(sess, emblemStatus)
}

// SendClanAttention 匯出 sendClanAttention。
func SendClanAttention(sess *net.Session) {
	sendClanAttention(sess)
}
