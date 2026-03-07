package handler

import (
	"github.com/l1jgo/server/internal/net"
	"github.com/l1jgo/server/internal/net/packet"
	"github.com/l1jgo/server/internal/world"
)

// --- 料理系統 ---
// Java: Cooking_Book.java, L1Cooking.java, S_PacketBoxCooking.java
// 料理書物品 ID: 41255-41259（5 級）
// S_PacketBoxCooking: opcode 250 (S_OPCODE_EVENT), type 52=選單, type 53=圖示

// 料理相關常數
const (
	cookingBookMinID int32 = 41255
	cookingBookMaxID int32 = 41259
	cookingDuration  int16 = 900  // 料理效果持續 15 分鐘（秒）
	cookingNowSkill  int32 = 2999 // 製作中狀態 skill ID
)

// CookingRecipe 料理配方。
type CookingRecipe struct {
	MaterialID int32 // 材料物品 ID
	ResultID   int32 // 成功結果物品 ID
	SpecialID  int32 // 特別版結果物品 ID（5% 機率）
	BuffType   int   // 料理 buff type
}

// 料理配方表（Java: Cooking_Book.java 中的 24 種料理）
// Lv1 料理（8 種），材料 → 普通/幻想版
var cookingRecipes = []CookingRecipe{
	{40057, 41277, 41285, 0},   // cookNo=0: 麵包 → 烤麵包
	{41275, 41278, 41286, 1},   // cookNo=1
	{41276, 41279, 41287, 2},   // cookNo=2
	{41277, 41280, 41288, 3},   // cookNo=3
	{41278, 41281, 41289, 4},   // cookNo=4
	{41279, 41282, 41290, 5},   // cookNo=5
	{41280, 41283, 41291, 6},   // cookNo=6
	{41281, 41284, 41292, 7},   // cookNo=7
}

// IsCookingBook 檢查物品是否為料理書。
func IsCookingBook(itemID int32) bool {
	return itemID >= cookingBookMinID && itemID <= cookingBookMaxID
}

// HandleCookingBook 處理使用料理書。
// Java: Cooking_Book.execute() — data[0]=0 顯示選單, data[1]=cookNo 製作
func HandleCookingBook(sess *net.Session, player *world.PlayerInfo, item *world.InvItem, deps *Deps) {
	// 計算料理等級（0-4）
	cookLevel := item.ItemID - cookingBookMinID

	// 顯示料理選單（Java: S_PacketBoxCooking type 52）
	sendCookingMenu(sess, player, int(cookLevel))
}

// HandleCookingSelect 處理料理選擇（從 C_HYPERTEXT_INPUT_RESULT 觸發）。
// Java: Cooking_Book data[1] = cookNo
func HandleCookingSelect(sess *net.Session, player *world.PlayerInfo, cookNo int, deps *Deps) {
	if cookNo < 0 || cookNo >= len(cookingRecipes) {
		return
	}

	recipe := cookingRecipes[cookNo]

	// 檢查是否有材料
	if player.Inv == nil {
		SendServerMessage(sess, 280) // "材料不足"
		return
	}
	mat := player.Inv.FindByItemID(recipe.MaterialID)
	if mat == nil {
		SendServerMessage(sess, 280) // "材料不足"
		return
	}

	// 消耗材料
	deps.NpcSvc.ConsumeItem(sess, player, mat.ObjectID, 1)

	// TODO: 透過 ItemCreate 系統建立結果物品到背包
	_ = recipe.ResultID

	// 發送料理圖示效果
	sendCookingIcon(sess, player, recipe.BuffType, cookingDuration)
}

// sendCookingMenu 發送料理選單封包。
// Java: S_PacketBoxCooking type 52 — 選單 UI
func sendCookingMenu(sess *net.Session, player *world.PlayerInfo, cookLevel int) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteH(52) // type 52: COOKING_WINDOW（料理選單）
	w.WriteC(byte(cookLevel))
	sess.Send(w.Bytes())
}

// sendCookingIcon 發送料理效果圖示封包。
// Java: S_PacketBoxCooking type 53 — 料理圖示
// 格式: writeC(STR) + writeC(INT) + writeC(WIS) + writeC(DEX) + writeC(CON) + writeC(CHA)
//       + writeH(food) + writeC(type) + writeC(icon_id) + writeH(time) + writeC(0)
func sendCookingIcon(sess *net.Session, player *world.PlayerInfo, cookType int, duration int16) {
	w := packet.NewWriterWithOpcode(packet.S_OPCODE_EVENT)
	w.WriteH(53) // type 53: ICON_COOKING

	// 基礎六維屬性
	w.WriteC(byte(player.Str))
	w.WriteC(byte(player.Intel))
	w.WriteC(byte(player.Wis))
	w.WriteC(byte(player.Dex))
	w.WriteC(byte(player.Con))
	w.WriteC(byte(player.Cha))

	// 飽食度計算（Java: food = pc.get_food() * 10 - 250，最小 0）
	food := int(player.Food)*10 - 250
	if food < 0 {
		food = 0
	}
	w.WriteH(uint16(food))

	w.WriteC(byte(cookType)) // 料理 type
	w.WriteC(38)             // icon_id（Java 預設 38）
	w.WriteH(uint16(duration))
	w.WriteC(0)              // 尾碼

	sess.Send(w.Bytes())
}
