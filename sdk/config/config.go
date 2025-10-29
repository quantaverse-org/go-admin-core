package config

import (
	"fmt"
	"log"

	"github.com/go-admin-team/go-admin-core/config"
	"github.com/go-admin-team/go-admin-core/config/source"
)

var (
	ExtendConfig interface{}
	_cfg         *Settings
)

// Settings 兼容原先的配置结构
type Settings struct {
	Settings  Config `yaml:"settings"`
	callbacks []func()
}

func (e *Settings) runCallback() {
	for i := range e.callbacks {
		e.callbacks[i]()
	}
}

func (e *Settings) OnChange() {
	e.init()
	log.Println("config change and reload")
}

func (e *Settings) Init() {
	e.init()
	log.Println("config init")
}

func (e *Settings) init() {
	// 🔥 关键修复：在调用 Setup 前，确保使用正确的 Logger 对象
	log.Printf("\n=== Settings.init() 调用 ===")
	log.Printf("e.Settings.Logger 地址: %p", e.Settings.Logger)
	log.Printf("LoggerConfig 地址: %p", LoggerConfig)

	if e.Settings.Logger == nil {
		log.Printf("⚠️ Logger 是 nil，使用 LoggerConfig")
		e.Settings.Logger = LoggerConfig
	} else if e.Settings.Logger != LoggerConfig {
		log.Printf("⚠️ YAML 解析器创建了新对象，同步数据")
		log.Printf("  新对象值: Path=%q, Level=%q", e.Settings.Logger.Path, e.Settings.Logger.Level)
		*LoggerConfig = *e.Settings.Logger
		e.Settings.Logger = LoggerConfig
		log.Printf("  同步后 LoggerConfig: Path=%q, Level=%q", LoggerConfig.Path, LoggerConfig.Level)
	} else {
		log.Printf("✓ Logger 对象正确指向 LoggerConfig")
		log.Printf("  值: Path=%q, Level=%q", LoggerConfig.Path, LoggerConfig.Level)
	}

	log.Printf("=== 调用 Logger.Setup() ===\n")
	e.Settings.Logger.Setup()
	e.Settings.multiDatabase()
	e.runCallback()
}

// Config 配置集合
type Config struct {
	Application *Application          `yaml:"application"`
	Ssl         *Ssl                  `yaml:"ssl"`
	Logger      *Logger               `yaml:"logger"`
	Jwt         *Jwt                  `yaml:"jwt"`
	Database    *Database             `yaml:"database"`
	Databases   *map[string]*Database `yaml:"databases"`
	Gen         *Gen                  `yaml:"gen"`
	Cache       *Cache                `yaml:"cache"`
	Queue       *Queue                `yaml:"queue"`
	Locker      *Locker               `yaml:"locker"`
	Extend      interface{}           `yaml:"extend"`
}

// 多db改造
func (e *Config) multiDatabase() {
	if len(*e.Databases) == 0 {
		*e.Databases = map[string]*Database{
			"*": e.Database,
		}

	}
}

// Setup 载入配置文件
func Setup(s source.Source,
	fs ...func()) {
	_cfg = &Settings{
		Settings: Config{
			Application: ApplicationConfig,
			Ssl:         SslConfig,
			Logger:      LoggerConfig,
			Jwt:         JwtConfig,
			Database:    DatabaseConfig,
			Databases:   &DatabasesConfig,
			Gen:         GenConfig,
			Cache:       CacheConfig,
			Queue:       QueueConfig,
			Locker:      LockerConfig,
			Extend:      ExtendConfig,
		},
		callbacks: fs,
	}
	var err error

	// 🔥 记录配置加载前的状态
	log.Printf("\n=== 配置加载前状态 ===")
	log.Printf("LoggerConfig 地址: %p", LoggerConfig)
	log.Printf("LoggerConfig 初始值: Path=%q, Level=%q, Stdout=%q", LoggerConfig.Path, LoggerConfig.Level, LoggerConfig.Stdout)
	log.Printf("_cfg.Settings.Logger 初始化时指向: %p (应该是 LoggerConfig 的地址)", LoggerConfig)

	config.DefaultConfig, err = config.NewConfig(
		config.WithSource(s),
		config.WithEntity(_cfg),
	)
	if err != nil {
		log.Fatal(fmt.Sprintf("New config object fail: %s", err.Error()))
	}

	// 🔥 关键修复：YAML 解析后，确保 LoggerConfig 被正确同步
	log.Printf("\n=== config.NewConfig() 后检查 ===")
	log.Printf("_cfg.Settings.Logger 地址: %p", _cfg.Settings.Logger)
	log.Printf("LoggerConfig 地址: %p", LoggerConfig)

	if _cfg.Settings.Logger == nil {
		log.Printf("✗ _cfg.Settings.Logger 是 nil!")
		_cfg.Settings.Logger = LoggerConfig
		log.Printf("  → 已设置为 LoggerConfig")
	} else if _cfg.Settings.Logger == LoggerConfig {
		log.Printf("✓ _cfg.Settings.Logger 和 LoggerConfig 是同一个对象")
		log.Printf("LoggerConfig 的值:")
		log.Printf("  Path: %q (长度: %d)", LoggerConfig.Path, len(LoggerConfig.Path))
		log.Printf("  Level: %q (长度: %d)", LoggerConfig.Level, len(LoggerConfig.Level))
		log.Printf("  Stdout: %q (长度: %d)", LoggerConfig.Stdout, len(LoggerConfig.Stdout))
		log.Printf("  Type: %q", LoggerConfig.Type)
		log.Printf("  EnabledDB: %v", LoggerConfig.EnabledDB)

		// 如果值仍为空，说明 YAML 解析可能失败
		if LoggerConfig.Path == "" && LoggerConfig.Level == "" && LoggerConfig.Type == "" {
			log.Printf("⚠️ 警告: 对象相同但所有值为空，YAML 解析可能失败或配置未找到")
		}
	} else {
		log.Printf("⚠️ YAML 解析器创建了新的 Logger 对象！")
		log.Printf("  旧对象地址: %p (LoggerConfig)", LoggerConfig)
		log.Printf("  新对象地址: %p (_cfg.Settings.Logger)", _cfg.Settings.Logger)
		log.Printf("")
		log.Printf("新对象的值:")
		log.Printf("  Path: %q (长度: %d)", _cfg.Settings.Logger.Path, len(_cfg.Settings.Logger.Path))
		log.Printf("  Level: %q (长度: %d)", _cfg.Settings.Logger.Level, len(_cfg.Settings.Logger.Level))
		log.Printf("  Stdout: %q (长度: %d)", _cfg.Settings.Logger.Stdout, len(_cfg.Settings.Logger.Stdout))
		log.Printf("  Type: %q", _cfg.Settings.Logger.Type)
		log.Printf("  EnabledDB: %v", _cfg.Settings.Logger.EnabledDB)
		log.Printf("")
		log.Printf("  → 同步新对象的值到 LoggerConfig")
		*LoggerConfig = *_cfg.Settings.Logger
		_cfg.Settings.Logger = LoggerConfig
		log.Printf("  同步后 LoggerConfig.Path: %q", LoggerConfig.Path)
		log.Printf("  同步后 LoggerConfig.Level: %q", LoggerConfig.Level)
	}
	log.Printf("========================\n")

	_cfg.Init()
}
