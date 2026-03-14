# 待推送變更

- fix: 修復 language=5（簡體客戶端）移動系統 heading XOR 問題 — Java C_MoveChar 僅 language=3 時 XOR 0x49，Go 無條件套用導致簡體端所有移動被拒絕（heading 值 >7），造成 NPC/怪物不可見 + 無法互動
- fix: S_ServerVersion 封包補上 Java 尾部 writeC(0) 缺少的位元組
- fix: 限時地圖 ID 修正為 3.80C 標準值（龍之谷、古魯丁、奇岩、象牙塔、傲慢之塔、拉斯塔巴德）
- fix: 限時地圖計時器改為每秒更新（匹配 Java CheckTimeController）
- fix: 沙哈之弓（item_id=190）無箭時可發射魔法箭（GfxID=2349），不消耗箭矢（匹配 Java C_AttackBow "$1821" 特殊處理）
- fix: 盾牌↔腰帶互斥修正為盾牌↔臂甲互斥（Java: type 7 ↔ type 13，腰帶不參與互斥）
- fix: 魂體轉換（技能 146）邏輯修正為增加 MP +12（Java: BLOODY_SOUL），原錯誤邏輯為 MP 轉 HP
- fix: 藍色藥水（40015）和慎重藥水（40016）從 item_vip.yaml 移除 — 錯誤的 VIP 配置攔截了正常藥水邏輯
- feat: 新增 ChargeCount 基礎設施（DB migration + InvItem 欄位 + 持久化 + 封包傳送）
- feat: 實現創造怪物魔杖（item_id 40006/140006）— 使用後隨機召喚 25 種怪物之一，扣減充能次數，用完自動刪除
- fix: 怪物聚堆修復 — 還原 spawn_list.yaml（轉換腳本白名單過濾導致 NPC 消失），改由 Go 代碼自動套用 ±3 格隨機範圍（count>1 且 randomx=0 時）
