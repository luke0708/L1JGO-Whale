# 待推送變更

## Bug 修復

- 修復心靈轉換（技能 130）：使用後 MP 全恢復 → 改為固定 +2 MP（匹配 Java BODY_TO_MIND）
- 修復魂體轉換（技能 146）：實際恢復 12MP 與客戶端提示「恢復19MP」不符 → 改用 skill_level=19 匹配客戶端顯示
- 修復商店購買魔杖時 charge_count 未寫入 DB → BuyFromNpc 加入 ChargeCount 初始化
- 修復祝福卷軸只有 +1 效果：移除 enchantScrollBless 中對 40074/40087 的硬編碼覆蓋，YAML 模板 bless 改為 0
- 新增萬能藥（40033-40038）使用功能：永久 +1 對應屬性（STR/CON/DEX/INT/WIS/CHA），上限 45，總使用次數上限 20

### 批次 B：技能系統修復

- 修復 AoE 技能重複施法動畫：改為主目標發 S_UseAttackSkill，次要目標只發 S_SkillEffect
- 修復冰雪颶風（skill 80）凍結施法者：移除 buffs.lua 中錯誤的 `[80] = { paralyzed = true }` 自身 buff
- 修復寒冰氣息（skill 22）不應有凍結效果：從兩處凍結判定移除 skill 22（Java 無此凍結）
- 修復冰矛圍籬（skill 50）無凍結效果：加入 executeAttackSkill 凍結判定
- 修復絕對屏障（skill 78）無特效：加入 cast_gfx 2234 廣播
- 修復魔法相消術（skill 44）NON_CANCELLABLE 過大：移除 Java 中不存在的 [33]/[78]/[157]
- 新增隱身術（skill 60）：設定 Invisible + S_Invis + 廣播 S_RemoveObject
- 修復起死回生術（skill 18）路由錯誤：從 RESURRECT_EFFECTS 移除（Java 中是 TURN_UNDEAD 不死族即死）
- 新增返生術（skill 61）：50% HP 復活 + S_Message_YN 同意對話（msgID 321）
- 修復終極返生術（skill 75）無同意對話：加入 S_Message_YN（msgID 322）+ 待同意機制
- 新增極光雷電（skill 17）直線目標：實作 Bresenham 演算法，從施法者到目標的直線上所有 NPC 受傷害
- 放寬自身 AoE 傷害條件：DamageValue > 0 → DamageValue > 0 || DamageDice > 0（覆蓋龍捲風/震裂術/火風暴/冰雪颶風）
