# 待推送變更

## Bug 修復

- 修復心靈轉換（技能 130）：使用後 MP 全恢復 → 改為固定 +2 MP（匹配 Java BODY_TO_MIND）
- 修復魂體轉換（技能 146）：實際恢復 12MP 與客戶端提示「恢復19MP」不符 → 改用 skill_level=19 匹配客戶端顯示
- 修復商店購買魔杖時 charge_count 未寫入 DB → BuyFromNpc 加入 ChargeCount 初始化
- 修復祝福卷軸只有 +1 效果：移除 enchantScrollBless 中對 40074/40087 的硬編碼覆蓋，YAML 模板 bless 改為 0
- 新增萬能藥（40033-40038）使用功能：永久 +1 對應屬性（STR/CON/DEX/INT/WIS/CHA），上限 45，總使用次數上限 20
