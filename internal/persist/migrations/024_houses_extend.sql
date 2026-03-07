-- +goose Up
-- 024: 擴充住宅表 — 新增管家、地下盟屋、稅期等動態欄位

ALTER TABLE houses
    ADD COLUMN IF NOT EXISTS keeper_id INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_purchase_basement BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS tax_deadline TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 days';

-- 從 YAML 靜態資料同步 keeper_id（啟動時由 Go 程式碼執行 SyncKeeperIDs）

-- +goose Down
ALTER TABLE houses
    DROP COLUMN IF EXISTS keeper_id,
    DROP COLUMN IF EXISTS is_purchase_basement,
    DROP COLUMN IF EXISTS tax_deadline;
