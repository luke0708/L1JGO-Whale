# 待推送變更

## 批次 C — 裝備/寵物/料理修復

### C1. 變身時可正常裝備耳環
- `data/polymorph.go`: `IsArmorEquipable()` 未知裝備類型（如 earring）預設允許裝備，不再誤攔

### C3. 隱身斗篷穿脫觸發隱形效果
- `system/equip.go`: 穿上隱身斗篷（20077/120077）自動設定隱身、廣播移除；脫下後解除隱身、廣播重現
- 新增 `applyInvisCloak()` 方法

### C4. 寵物/召喚物安全區內不主動攻擊
- `system/companion_ai.go`: `summonScanForTarget()` 和 `petScanForTarget()` 加入 `IsSafetyZone` 檢查

### C5. 料理 buff 效果系統
- `system/item_use.go`: 新增 `cookingBuffMap`（Lv1-Lv4 共 35 種料理 → buff 映射）和 `applyCookingBuff()`
- 料理使用後除飽食度外，套用對應 buff（AC/HP/MP/MR/SP/HPR/MPR/元素抗性）
- 同時只能有一個料理 buff，新料理自動覆蓋舊 buff
- 發送屬性更新封包 + 料理圖示
- `world/state.go`: 新增 `CookingID` 欄位追蹤當前料理 buff
- `handler/cooking.go`: 匯出 `SendCookingIcon()`

### C6. 寵物改名路由修復
- `handler/attr.go`: 新增 C_Attr mode 325 處理（寵物改名 Yes/No + 新名稱輸入）
- `system/pet_mgr.go`: changename 動作時暫存 `player.TempID = pet.ID`
- `world/state.go`: 新增 `TempID` 欄位（暫存目標 ID）
