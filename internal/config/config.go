package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server      ServerConfig      `toml:"server"`
	Database    DatabaseConfig    `toml:"database"`
	Persistence PersistenceConfig `toml:"persistence"`
	Network     NetworkConfig     `toml:"network"`
	Rates       RatesConfig       `toml:"rates"`
	Enchant     EnchantConfig     `toml:"enchant"`
	World       WorldConfig       `toml:"world"`
	Character   CharacterConfig   `toml:"character"`
	Gameplay    GameplayConfig    `toml:"gameplay"`
	Lua         LuaConfig         `toml:"lua"`
	AntiCheat   AntiCheatConfig   `toml:"anti_cheat"`
	Debug       DebugConfig       `toml:"debug"`
	Logging     LoggingConfig     `toml:"logging"`
	RateLimit   RateLimitConfig   `toml:"rate_limit"`
}

type PersistenceConfig struct {
	BatchIntervalTicks int    `toml:"batch_interval_ticks"` // auto-save every N ticks (default 1500 = 5 min)
	WALSyncMode        string `toml:"wal_sync_mode"`        // "sync" or "async" (default "sync")
}

type WorldConfig struct {
	WeatherEnabled   bool `toml:"weather_enabled"`
	WeatherInterval  int  `toml:"weather_interval_ticks"` // ticks between weather changes
	GroundItemExpiry int  `toml:"ground_item_expiry"`     // ticks before ground items expire
}

type LuaConfig struct {
	TickBudgetPct float64       `toml:"tick_budget_pct"` // max % of tick time for Lua (0.0-1.0)
	Timeout       time.Duration `toml:"timeout"`         // per-call Lua timeout
	MemoryLimitMB int           `toml:"memory_limit_mb"` // Lua VM memory limit
}

type AntiCheatConfig struct {
	SpeedThreshold      float64 `toml:"speed_threshold"`      // max tiles/second before flagging
	TeleportValidation  bool    `toml:"teleport_validation"`  // validate teleport destinations
	DuplicateItemCheck  bool    `toml:"duplicate_item_check"` // detect duplicated item IDs
}

type EnchantConfig struct {
	WeaponChance float64 `toml:"weapon_chance"` // success rate above safe enchant (0.0-1.0)
	ArmorChance  float64 `toml:"armor_chance"`  // success rate above safe enchant (0.0-1.0)
}

type ServerConfig struct {
	Name      string `toml:"name"`
	ID        int    `toml:"id"`
	Language  int    `toml:"language"` // 0=US, 3=Taiwan, 4=Japan, 5=China
	StartTime int64  // set at boot, not from config
}

type DatabaseConfig struct {
	DSN             string        `toml:"dsn"`
	MaxOpenConns    int           `toml:"max_open_conns"`
	MaxIdleConns    int           `toml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `toml:"conn_max_lifetime"`
}

type NetworkConfig struct {
	BindAddress       string        `toml:"bind_address"`
	TickRate          time.Duration `toml:"tick_rate"`
	InQueueSize       int           `toml:"in_queue_size"`
	OutQueueSize      int           `toml:"out_queue_size"`
	MaxPacketsPerTick int           `toml:"max_packets_per_tick"`
	WriteTimeout      time.Duration `toml:"write_timeout"`
	ReadTimeout       time.Duration `toml:"read_timeout"`
}

type RatesConfig struct {
	ExpRate    float64 `toml:"exp_rate"`
	DropRate   float64 `toml:"drop_rate"`
	GoldRate   float64 `toml:"gold_rate"`
	LawfulRate float64 `toml:"lawful_rate"`
	PetExpRate float64 `toml:"pet_exp_rate"`
}

type CharacterConfig struct {
	DefaultSlots         int    `toml:"default_slots"`
	AutoCreateAccounts   bool   `toml:"auto_create_accounts"`
	Delete7Days          bool   `toml:"delete_7_days"`
	Delete7DaysMinLevel  int    `toml:"delete_7_days_min_level"`
	ClientLanguageCode   string `toml:"client_language_code"`
	ChangeTitleByOneself bool   `toml:"change_title_by_oneself"`
}

// GameplayConfig holds tunable game constants that server admins may want to adjust.
// Previously these were scattered as magic numbers across handler code.
type GameplayConfig struct {
	// Board (bulletin board)
	BoardPostCost int `toml:"board_post_cost"` // adena cost to write a board post
	BoardPageSize int `toml:"board_page_size"` // posts per page

	// Mail
	MailSendCost int `toml:"mail_send_cost"` // adena cost to send mail
	MailMaxPerBox int `toml:"mail_max_per_box"` // max messages per mailbox type

	// Warehouse
	WarehousePersonalFee int `toml:"warehouse_personal_fee"` // adena per withdrawal
	WarehouseElfFee      int `toml:"warehouse_elf_fee"`      // mithril count per withdrawal

	// Repair
	RepairCostPerDurability int `toml:"repair_cost_per_durability"` // adena per durability point

	// Chat
	WorldChatMinFood int `toml:"world_chat_min_food"` // minimum food to world chat
	WorldChatFoodCost int `toml:"world_chat_food_cost"` // food consumed per world chat

	// PvP
	KillMessageLevel int `toml:"kill_message_level"` // min victim level for kill broadcast (0=disabled, default 90)

	// Exclude (block list)
	MaxExcludeList int `toml:"max_exclude_list"` // max entries in block list

	// Character defaults
	InitialFood    int `toml:"initial_food"`    // food on creation / respawn
	BaseAC         int `toml:"base_ac"`         // base AC for all characters
	MaxFoodSatiety int `toml:"max_food_satiety"` // food cap from eating
	HouseHPRBonus  int `toml:"house_hpr_bonus"`  // 血盟小屋 HP 回復加成
	HouseMPRBonus  int `toml:"house_mpr_bonus"`  // 血盟小屋 MP 回復加成
}

type DebugConfig struct {
	ShowNpcID bool `toml:"show_npc_id"` // NPC 名稱旁顯示 NPC ID 和 GFX ID
}

type LoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"` // "json" or "console"
}

type RateLimitConfig struct {
	Enabled                bool `toml:"enabled"`
	LoginAttemptsPerMinute int  `toml:"login_attempts_per_minute"`
	PacketsPerSecond       int  `toml:"packets_per_second"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := defaults()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Server.StartTime = time.Now().Unix()
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Name:     "L1JGO-Whale",
			ID:       1,
			Language: 3, // Taiwan
		},
		Database: DatabaseConfig{
			DSN:             "postgres://l1jgo:l1jgo@localhost:5432/l1jgo?sslmode=disable",
			MaxOpenConns:    20,
			MaxIdleConns:    5,
			ConnMaxLifetime: 30 * time.Minute,
		},
		Network: NetworkConfig{
			BindAddress:       "0.0.0.0:7001",
			TickRate:          200 * time.Millisecond,
			InQueueSize:       128,
			OutQueueSize:      2048,
			MaxPacketsPerTick: 32,
			WriteTimeout:      10 * time.Second,
			ReadTimeout:       60 * time.Second,
		},
		Persistence: PersistenceConfig{
			BatchIntervalTicks: 1500,   // 5 minutes at 200ms/tick
			WALSyncMode:        "sync", // synchronous WAL writes
		},
		Rates: RatesConfig{
			ExpRate:    1.0,
			DropRate:   1.0,
			GoldRate:   1.0,
			LawfulRate: 1.0,
			PetExpRate: 1.0,
		},
		Enchant: EnchantConfig{
			WeaponChance: 0.68, // Java default ENCHANT_CHANCE_WEAPON = 68
			ArmorChance:  0.52, // Java default ENCHANT_CHANCE_ARMOR = 52
		},
		World: WorldConfig{
			WeatherEnabled:   true,
			WeatherInterval:  100, // ~20 seconds at 200ms/tick
			GroundItemExpiry: 300, // ~60 seconds
		},
		Character: CharacterConfig{
			DefaultSlots:         6,
			AutoCreateAccounts:   true,
			Delete7Days:          true,
			Delete7DaysMinLevel:  5,
			ClientLanguageCode:   "MS950",
			ChangeTitleByOneself: true,
		},
		Gameplay: GameplayConfig{
			BoardPostCost:          300,
			BoardPageSize:          8,
			MailSendCost:           50,
			MailMaxPerBox:          40,
			WarehousePersonalFee:   30,
			WarehouseElfFee:        2,
			RepairCostPerDurability: 200,
			WorldChatMinFood:       6,
			WorldChatFoodCost:      5,
			MaxExcludeList:         16,
			InitialFood:            40,
			BaseAC:                 10,
			MaxFoodSatiety:         225,
			HouseHPRBonus:          10,
			HouseMPRBonus:          10,
		},
		Lua: LuaConfig{
			TickBudgetPct: 0.50,                   // warn if Lua uses > 50% of tick
			Timeout:       100 * time.Millisecond,  // per-call timeout
			MemoryLimitMB: 64,                      // 64 MB VM memory
		},
		AntiCheat: AntiCheatConfig{
			SpeedThreshold:     15.0, // tiles/second (normal walk ~5, haste ~8)
			TeleportValidation: true,
			DuplicateItemCheck: true,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "console",
		},
		RateLimit: RateLimitConfig{
			Enabled:                true,
			LoginAttemptsPerMinute: 10,
			PacketsPerSecond:       60,
		},
	}
}
