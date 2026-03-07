-- +goose Up
-- 022: 社交+生活系統基礎表
-- 結婚、聯盟、血盟配對、排名所需的資料庫變更

-- 結婚系統：角色表新增配偶 ID
ALTER TABLE characters ADD COLUMN IF NOT EXISTS partner_id INT NOT NULL DEFAULT 0;

-- 排名系統：擊殺/死亡計數（線上累計，定期存檔）
ALTER TABLE characters ADD COLUMN IF NOT EXISTS kill_count INT NOT NULL DEFAULT 0;
ALTER TABLE characters ADD COLUMN IF NOT EXISTS death_count INT NOT NULL DEFAULT 0;

-- 血盟聯盟表（最多 4 個血盟組成聯盟）
CREATE TABLE IF NOT EXISTS character_alliance (
    order_id SERIAL PRIMARY KEY,
    alliance_id1 INT NOT NULL DEFAULT 0,
    alliance_id2 INT NOT NULL DEFAULT 0,
    alliance_id3 INT NOT NULL DEFAULT 0,
    alliance_id4 INT NOT NULL DEFAULT 0
);

-- 血盟配對：推薦登錄
CREATE TABLE IF NOT EXISTS clan_matching_list (
    clanname VARCHAR(45) PRIMARY KEY,
    text VARCHAR(255) NOT NULL DEFAULT '',
    type INT NOT NULL DEFAULT 0
);

-- 血盟配對：申請列表
CREATE TABLE IF NOT EXISTS clan_matching_apclist (
    id SERIAL PRIMARY KEY,
    pc_name VARCHAR(45) NOT NULL,
    pc_objid INT NOT NULL,
    clan_name VARCHAR(45) NOT NULL,
    clan_id INT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_clan_matching_apc_clan ON clan_matching_apclist(clan_id);
CREATE INDEX IF NOT EXISTS idx_clan_matching_apc_pc ON clan_matching_apclist(pc_objid);

-- +goose Down
DROP INDEX IF EXISTS idx_clan_matching_apc_pc;
DROP INDEX IF EXISTS idx_clan_matching_apc_clan;
DROP TABLE IF EXISTS clan_matching_apclist;
DROP TABLE IF EXISTS clan_matching_list;
DROP TABLE IF EXISTS character_alliance;
ALTER TABLE characters DROP COLUMN IF EXISTS death_count;
ALTER TABLE characters DROP COLUMN IF EXISTS kill_count;
ALTER TABLE characters DROP COLUMN IF EXISTS partner_id;
