package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
	"go.uber.org/zap"
)

// --- 血盟配對系統（Clan Matching） ---
// Java: C_ClanMatching.java (opcode 76), S_ClanMatching.java (opcode 0)
// 7 種操作：登錄/取消/推薦目錄/已申請/邀請/申請加入/許可拒絕

// ClanMatchingEntry 血盟配對登錄資料（記憶體快取）。
type ClanMatchingEntry struct {
	ClanName string
	Text     string // 血盟介紹文
	Type     int    // 0=戰鬥, 1=狩獵, 2=友好
}

// ClanMatchingApply 血盟配對申請記錄。
type ClanMatchingApply struct {
	PCName  string
	PCObjID int32
	ClanName string
	ClanID  int32
}

// ClanMatchingManager 管理血盟配對資料。
type ClanMatchingManager struct {
	listings map[string]*ClanMatchingEntry   // clanName → entry
	applies  map[int32][]*ClanMatchingApply  // clanID → applies
}

// NewClanMatchingManager 建構血盟配對管理器。
func NewClanMatchingManager() *ClanMatchingManager {
	return &ClanMatchingManager{
		listings: make(map[string]*ClanMatchingEntry),
		applies:  make(map[int32][]*ClanMatchingApply),
	}
}

// AddListing 新增血盟登錄。
func (m *ClanMatchingManager) AddListing(entry *ClanMatchingEntry) {
	m.listings[entry.ClanName] = entry
}

// RemoveListing 移除血盟登錄。
func (m *ClanMatchingManager) RemoveListing(clanName string) {
	delete(m.listings, clanName)
}

// GetListing 取得血盟登錄資料。
func (m *ClanMatchingManager) GetListing(clanName string) *ClanMatchingEntry {
	return m.listings[clanName]
}

// GetAllListings 取得所有登錄血盟。
func (m *ClanMatchingManager) GetAllListings() []*ClanMatchingEntry {
	result := make([]*ClanMatchingEntry, 0, len(m.listings))
	for _, e := range m.listings {
		result = append(result, e)
	}
	return result
}

// AddApply 新增申請記錄。
func (m *ClanMatchingManager) AddApply(apply *ClanMatchingApply) {
	m.applies[apply.ClanID] = append(m.applies[apply.ClanID], apply)
}

// GetApplies 取得血盟收到的所有申請。
func (m *ClanMatchingManager) GetApplies(clanID int32) []*ClanMatchingApply {
	return m.applies[clanID]
}

// RemoveApply 移除申請記錄。
func (m *ClanMatchingManager) RemoveApply(clanID int32, pcObjID int32) {
	list := m.applies[clanID]
	for i, a := range list {
		if a.PCObjID == pcObjID {
			m.applies[clanID] = append(list[:i], list[i+1:]...)
			return
		}
	}
}

// HandleClanMatching 處理 C_ClanMatching（opcode 76）。
// Java: C_ClanMatching.java — readC() = type (0-6)
func HandleClanMatching(sess *net.Session, r *packet.Reader, deps *Deps) {
	opType := r.ReadC()

	player := deps.World.GetBySession(sess.ID)
	if player == nil {
		return
	}

	deps.Log.Debug("C_ClanMatching",
		zap.String("player", player.Name),
		zap.Uint8("type", opType),
	)

	if deps.ClanMatching == nil {
		return
	}

	switch opType {
	case 0: // 推薦血盟登錄/修改
		handleClanMatchingRegister(sess, r, player, deps)

	case 1: // 取消登錄
		handleClanMatchingCancel(sess, player, deps)

	case 2: // 打開推薦血盟目錄
		handleClanMatchingBrowse(sess, player, deps)

	case 3: // 打開已申請目錄
		handleClanMatchingMyApplies(sess, player, deps)

	case 4: // 打開邀請目錄（盟主查看申請列表）
		handleClanMatchingInvites(sess, player, deps)

	case 5: // 申請加入
		clanID := r.ReadD()
		handleClanMatchingApply(sess, player, clanID, deps)

	case 6: // 加入許可/拒絕/取消
		targetID := r.ReadD()
		subType := r.ReadC() // 1=許可, 2=拒絕, 3=自行取消
		handleClanMatchingDecision(sess, player, targetID, subType, deps)
	}
}

// handleClanMatchingRegister 處理血盟登錄（type=0）。
func handleClanMatchingRegister(sess *net.Session, r *packet.Reader, player *world.PlayerInfo, deps *Deps) {
	clanType := r.ReadC() // 0=戰鬥, 1=狩獵, 2=友好
	text := r.ReadS()

	if player.ClanID == 0 {
		return
	}

	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil || clan.LeaderID != player.CharID {
		return
	}

	entry := &ClanMatchingEntry{
		ClanName: clan.ClanName,
		Text:     text,
		Type:     int(clanType),
	}
	deps.ClanMatching.AddListing(entry)

	// 回應成功（Java: S_ClanMatching(true, clanname)）
	sendClanMatchingResult(sess, 0) // status=0 成功
}

// handleClanMatchingCancel 處理取消登錄（type=1）。
func handleClanMatchingCancel(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.ClanID == 0 {
		return
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil {
		return
	}

	deps.ClanMatching.RemoveListing(clan.ClanName)
	sendClanMatchingResult(sess, 1) // status=1 取消
}

// handleClanMatchingBrowse 打開推薦血盟目錄（type=2）。
func handleClanMatchingBrowse(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	listings := deps.ClanMatching.GetAllListings()

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(2)                  // type=2
	w.WriteC(0)                  // padding
	w.WriteC(byte(len(listings))) // count

	for _, entry := range listings {
		clan := deps.World.Clans.GetClanByName(entry.ClanName)
		if clan == nil {
			continue
		}
		w.WriteD(clan.ClanID)    // clan_id
		w.WriteS(clan.ClanName)  // clan_name
		w.WriteS(clan.LeaderName) // leader_name

		// 計算線上人數
		online := int32(0)
		for charID := range clan.Members {
			if deps.World.GetByCharID(charID) != nil {
				online++
			}
		}
		w.WriteD(online)         // online_count
		w.WriteC(byte(entry.Type)) // clan_type
		w.WriteC(boolByte(clan.HasCastle > 0)) // has_house
		w.WriteC(0)              // in_war (暫時固定 0)
		w.WriteC(0)              // padding
		w.WriteS(entry.Text)     // clan_desc
		w.WriteD(clan.EmblemID)  // emblem_id
	}

	sess.Send(w.Bytes())
}

// handleClanMatchingMyApplies 打開已申請目錄（type=3）。
func handleClanMatchingMyApplies(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	// 簡化版：回傳空列表
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(3)  // type=3
	w.WriteC(0)  // padding
	w.WriteC(0)  // count=0
	sess.Send(w.Bytes())
}

// handleClanMatchingInvites 打開邀請目錄（type=4，盟主查看申請列表）。
func handleClanMatchingInvites(sess *net.Session, player *world.PlayerInfo, deps *Deps) {
	if player.ClanID == 0 {
		return
	}
	clan := deps.World.Clans.GetClan(player.ClanID)
	if clan == nil {
		return
	}

	// 檢查是否已登錄
	listing := deps.ClanMatching.GetListing(clan.ClanName)
	if listing == nil {
		// 未登錄：回傳 error_code=130
		w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
		w.WriteC(4)   // type=4
		w.WriteC(130) // error_code：未登錄
		sess.Send(w.Bytes())
		return
	}

	applies := deps.ClanMatching.GetApplies(player.ClanID)

	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(4) // type=4
	w.WriteC(0) // padding
	w.WriteC(2) // padding（Java: 固定寫 2）
	w.WriteC(0) // padding
	w.WriteC(byte(len(applies))) // count

	for _, apply := range applies {
		applicant := deps.World.GetByCharID(apply.PCObjID)
		w.WriteD(apply.PCObjID) // player_id
		w.WriteC(0)             // padding
		if applicant != nil {
			w.WriteC(1) // online
			w.WriteS(applicant.Name)
			w.WriteC(byte(applicant.ClassType))
			w.WriteH(uint16(applicant.Lawful))
			w.WriteC(byte(applicant.Level))
		} else {
			w.WriteC(0)            // offline
			w.WriteS(apply.PCName) // 用存儲的名稱
			w.WriteC(0)            // class
			w.WriteH(uint16(0))    // lawful
			w.WriteC(0)            // level
		}
		w.WriteC(1) // padding（Java: 固定寫 1）
	}

	sess.Send(w.Bytes())
}

// handleClanMatchingApply 申請加入血盟（type=5）。
func handleClanMatchingApply(sess *net.Session, player *world.PlayerInfo, clanID int32, deps *Deps) {
	if player.ClanID != 0 {
		SendServerMessage(sess, 89) // "你已經有血盟了"
		return
	}

	clan := deps.World.Clans.GetClan(clanID)
	if clan == nil {
		return
	}

	apply := &ClanMatchingApply{
		PCName:   player.Name,
		PCObjID:  player.CharID,
		ClanName: clan.ClanName,
		ClanID:   clanID,
	}
	deps.ClanMatching.AddApply(apply)

	// 回應客戶端
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(5)       // type=5
	w.WriteC(0)       // padding
	w.WriteD(clanID)  // target_id
	w.WriteC(0)       // htype
	sess.Send(w.Bytes())
}

// handleClanMatchingDecision 處理加入許可/拒絕/取消（type=6）。
func handleClanMatchingDecision(sess *net.Session, player *world.PlayerInfo, targetID int32, subType uint8, deps *Deps) {
	switch subType {
	case 1: // 許可
		// 透過 JoinResponse 流程加入血盟（模擬盟主同意加入）
		if deps.Clan != nil && player.ClanID != 0 {
			deps.Clan.JoinResponse(sess, player, targetID, true)
		}
		deps.ClanMatching.RemoveApply(player.ClanID, targetID)

	case 2: // 拒絕
		deps.ClanMatching.RemoveApply(player.ClanID, targetID)
		// 通知申請人（如果線上）
		applicant := deps.World.GetByCharID(targetID)
		if applicant != nil {
			SendServerMessage(applicant.Session, 3267) // "加入邀請被拒絕"
		}

	case 3: // 自行取消申請
		// 取消自己對某血盟的申請
		deps.ClanMatching.RemoveApply(targetID, player.CharID)
	}

	// 回應客戶端
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(6)        // type=6
	w.WriteC(0)        // padding
	w.WriteD(targetID) // target_id
	w.WriteC(subType)  // htype
	sess.Send(w.Bytes())
}

// sendClanMatchingResult 發送血盟配對操作結果。
// Java: S_ClanMatching(boolean postStatus, String clanname)
func sendClanMatchingResult(sess *net.Session, status byte) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_CLANMATCHING)
	w.WriteC(status) // 0=成功, 1=取消
	w.WriteC(0)      // padding
	w.WriteD(0)      // padding
	w.WriteC(0)      // padding
	sess.Send(w.Bytes())
}

// boolByte 將 bool 轉為 byte。
func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
