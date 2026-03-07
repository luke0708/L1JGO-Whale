-- +goose Up
-- 025: 旅館系統 — 房間資料表 + 鑰匙物品欄位

CREATE TABLE IF NOT EXISTS inn_rooms (
    npc_id       INT     NOT NULL,
    room_number  INT     NOT NULL,
    key_id       INT     NOT NULL DEFAULT 0,
    lodger_id    INT     NOT NULL DEFAULT 0,
    hall         BOOLEAN NOT NULL DEFAULT FALSE,
    due_time     TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    PRIMARY KEY (npc_id, room_number)
);

ALTER TABLE character_items
    ADD COLUMN IF NOT EXISTS inn_key_id   INT     DEFAULT 0,
    ADD COLUMN IF NOT EXISTS inn_npc_id   INT     DEFAULT 0,
    ADD COLUMN IF NOT EXISTS inn_hall     BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS inn_due_time TIMESTAMPTZ;

-- +goose Down
DROP TABLE IF EXISTS inn_rooms;
ALTER TABLE character_items
    DROP COLUMN IF EXISTS inn_key_id,
    DROP COLUMN IF EXISTS inn_npc_id,
    DROP COLUMN IF EXISTS inn_hall,
    DROP COLUMN IF EXISTS inn_due_time;
