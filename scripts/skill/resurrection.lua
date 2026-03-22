-- Resurrection skill effect definitions
-- Returns hp_ratio/mp_ratio (0.0-1.0) or fixed_hp for fixed-amount heals

RESURRECT_EFFECTS = {
    -- 注意: skill 18（起死回生術）是 TURN_UNDEAD（不死族即死），不是復活技能
    [61]  = { fixed_hp = 0,  hp_ratio = 0.5, mp_ratio = 0 },   -- 返生術: 50% HP（Java: RESURRECTION）
    [75]  = { fixed_hp = 0,  hp_ratio = 1.0, mp_ratio = 1.0 }, -- 終極返生術: 全恢復（Java: GREATER_RESURRECTION）
    [131] = { fixed_hp = 0,  hp_ratio = 0.5, mp_ratio = 0.5 }, -- 世界樹的呼喚: 50% 恢復
    [165] = { fixed_hp = 0,  hp_ratio = 1.0, mp_ratio = 1.0 }, -- 生命呼喚: 全恢復
}

function get_resurrect_effect(skill_id)
    return RESURRECT_EFFECTS[skill_id]
end

-- Resurrection skill ID set (for routing check)
function is_resurrection_skill(skill_id)
    return RESURRECT_EFFECTS[skill_id] ~= nil
end
