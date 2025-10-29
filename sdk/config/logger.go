package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/go-admin-team/go-admin-core/sdk/pkg/logger"
)

type Logger struct {
	Type       string `yaml:"type"`
	Path       string `yaml:"path"`
	Level      string `yaml:"level"`
	Stdout     string `yaml:"stdout"`
	EnabledDB  bool   `yaml:"enableddb"`
	Cap        uint   `yaml:"cap"`
	DaysToKeep uint   `yaml:"daystokeep"`
}

// Setup 设置logger
func (e Logger) Setup() {
	// 添加调试日志
	log.Printf("=== Logger.Setup() 调用 ===")
	log.Printf("Logger 结构体内容:")
	log.Printf("  Type: %q", e.Type)
	log.Printf("  Path: %q (长度: %d)", e.Path, len(e.Path))
	log.Printf("  Level: %q", e.Level)
	log.Printf("  Stdout: %q", e.Stdout)
	log.Printf("  EnabledDB: %v", e.EnabledDB)
	log.Printf("  Cap: %d", e.Cap)
	log.Printf("  DaysToKeep: %d", e.DaysToKeep)

	// 🔥 强制修复：如果 Path 为空，使用绝对路径
	finalPath := e.Path
	if finalPath == "" || len(finalPath) == 0 {
		log.Printf("⚠️ 警告: Path 为空，尝试从环境变量或使用默认值")
		// 优先使用环境变量
		if envPath := os.Getenv("LOG_PATH"); envPath != "" {
			finalPath = envPath
			log.Printf("  使用环境变量 LOG_PATH: %q", finalPath)
		} else {
			// 使用绝对路径作为默认值（staging 环境）
			finalPath = "/app/temp/logs"
			log.Printf("  使用强制默认值: %q", finalPath)
		}
	} else {
		// 确保相对路径转换为绝对路径
		if !filepath.IsAbs(finalPath) {
			wd, _ := os.Getwd()
			finalPath = filepath.Join(wd, finalPath)
			log.Printf("  相对路径已转换为绝对路径: %q", finalPath)
		}
	}

	log.Printf("最终使用的路径: %q", finalPath)

	logger.SetupLogger(
		logger.WithType(e.Type),
		logger.WithPath(finalPath), // 使用修复后的路径
		logger.WithLevel(e.Level),
		logger.WithStdout(e.Stdout),
		logger.WithCap(e.Cap),
		logger.WithDaysToKeep(e.DaysToKeep),
	)
}

var LoggerConfig = new(Logger)
