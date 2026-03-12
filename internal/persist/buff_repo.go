package persist

import (
	"context"
	"fmt"
)

// BuffRow represents a single active buff persisted for a character.
type BuffRow struct {
	CharID        int32
	SkillID       int32
	RemainingTime int   // seconds
	PolyID        int32 // polymorph GFX (only for skill_id=67)
	DeltaAC       int16
	DeltaStr      int16
	DeltaDex      int16
	DeltaCon      int16
	DeltaWis      int16
	DeltaIntel    int16
	DeltaCha      int16
	DeltaMaxHP    int32
	DeltaMaxMP    int32
	DeltaHitMod   int16
	DeltaDmgMod   int16
	DeltaSP       int16
	DeltaMR       int16
	DeltaHPR      int16
	DeltaMPR      int16
	DeltaBowHit   int16
	DeltaBowDmg   int16
	DeltaFireRes  int16
	DeltaWaterRes int16
	DeltaWindRes  int16
	DeltaEarthRes int16
	DeltaDodge    int16
	SetMoveSpeed  byte
	SetBraveSpeed byte
}

// BuffRepo handles persistence of character active buffs.
type BuffRepo struct {
	db *DB
}

// NewBuffRepo creates a new BuffRepo.
func NewBuffRepo(db *DB) *BuffRepo {
	return &BuffRepo{db: db}
}

// LoadByCharID returns all persisted buffs for a character.
func (r *BuffRepo) LoadByCharID(ctx context.Context, charID int32) ([]BuffRow, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT char_id, skill_id, remaining_time, poly_id,
		        delta_ac, delta_str, delta_dex, delta_con, delta_wis, delta_intel, delta_cha,
		        delta_max_hp, delta_max_mp, delta_hit_mod, delta_dmg_mod,
		        delta_sp, delta_mr, delta_hpr, delta_mpr,
		        delta_bow_hit, delta_bow_dmg,
		        delta_fire_res, delta_water_res, delta_wind_res, delta_earth_res,
		        delta_dodge, set_move_speed, set_brave_speed
		 FROM character_buffs WHERE char_id = $1`, charID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BuffRow
	for rows.Next() {
		var b BuffRow
		if err := rows.Scan(
			&b.CharID, &b.SkillID, &b.RemainingTime, &b.PolyID,
			&b.DeltaAC, &b.DeltaStr, &b.DeltaDex, &b.DeltaCon, &b.DeltaWis, &b.DeltaIntel, &b.DeltaCha,
			&b.DeltaMaxHP, &b.DeltaMaxMP, &b.DeltaHitMod, &b.DeltaDmgMod,
			&b.DeltaSP, &b.DeltaMR, &b.DeltaHPR, &b.DeltaMPR,
			&b.DeltaBowHit, &b.DeltaBowDmg,
			&b.DeltaFireRes, &b.DeltaWaterRes, &b.DeltaWindRes, &b.DeltaEarthRes,
			&b.DeltaDodge, &b.SetMoveSpeed, &b.SetBraveSpeed,
		); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// SaveBuffs persists all active buffs for a character (replaces existing).
func (r *BuffRepo) SaveBuffs(ctx context.Context, charID int32, buffs []BuffRow) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin buff save: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear old buffs
	if _, err := tx.Exec(ctx, `DELETE FROM character_buffs WHERE char_id = $1`, charID); err != nil {
		return fmt.Errorf("delete old buffs: %w", err)
	}

	// Insert current buffs
	for i := range buffs {
		b := &buffs[i]
		if _, err := tx.Exec(ctx,
			`INSERT INTO character_buffs (
				char_id, skill_id, remaining_time, poly_id,
				delta_ac, delta_str, delta_dex, delta_con, delta_wis, delta_intel, delta_cha,
				delta_max_hp, delta_max_mp, delta_hit_mod, delta_dmg_mod,
				delta_sp, delta_mr, delta_hpr, delta_mpr,
				delta_bow_hit, delta_bow_dmg,
				delta_fire_res, delta_water_res, delta_wind_res, delta_earth_res,
				delta_dodge, set_move_speed, set_brave_speed
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)`,
			charID, b.SkillID, b.RemainingTime, b.PolyID,
			b.DeltaAC, b.DeltaStr, b.DeltaDex, b.DeltaCon, b.DeltaWis, b.DeltaIntel, b.DeltaCha,
			b.DeltaMaxHP, b.DeltaMaxMP, b.DeltaHitMod, b.DeltaDmgMod,
			b.DeltaSP, b.DeltaMR, b.DeltaHPR, b.DeltaMPR,
			b.DeltaBowHit, b.DeltaBowDmg,
			b.DeltaFireRes, b.DeltaWaterRes, b.DeltaWindRes, b.DeltaEarthRes,
			b.DeltaDodge, b.SetMoveSpeed, b.SetBraveSpeed,
		); err != nil {
			return fmt.Errorf("insert buff skill=%d: %w", b.SkillID, err)
		}
	}

	return tx.Commit(ctx)
}

// DeleteByCharID removes all persisted buffs for a character.
func (r *BuffRepo) DeleteByCharID(ctx context.Context, charID int32) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM character_buffs WHERE char_id = $1`, charID)
	return err
}
