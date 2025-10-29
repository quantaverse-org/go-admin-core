package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-admin-team/go-admin-core/debug/writer"
	"github.com/go-admin-team/go-admin-core/logger"
	"github.com/go-admin-team/go-admin-core/plugins/logger/zap"
	"github.com/go-admin-team/go-admin-core/sdk/pkg"
)

// SetupLogger 日志 cap 单位为kb
func SetupLogger(opts ...Option) logger.Logger {
	op := setDefault()
	for _, o := range opts {
		o(&op)
	}

	// 添加详细调试日志
	log.Printf("=== SetupLogger 调试信息 ===")
	log.Printf("配置的日志路径: %q", op.path)
	log.Printf("stdout 配置: %q", op.stdout)
	log.Printf("driver 配置: %q", op.driver)

	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("⚠️ 无法获取工作目录: %v", err)
	} else {
		log.Printf("当前工作目录: %q", wd)
	}

	// 判断路径是绝对路径还是相对路径
	if filepath.IsAbs(op.path) {
		log.Printf("路径类型: 绝对路径")
	} else {
		log.Printf("路径类型: 相对路径")
		resolvedPath := filepath.Join(wd, op.path)
		log.Printf("解析后的完整路径: %q", resolvedPath)
	}

	// 检查路径的父目录
	parentDir := filepath.Dir(op.path)
	log.Printf("父目录: %q", parentDir)
	if filepath.IsAbs(op.path) {
		if parentStat, err := os.Stat(parentDir); err != nil {
			log.Printf("⚠️ 父目录不存在或不可访问: %v", err)
		} else {
			log.Printf("✓ 父目录存在: %s, 是目录: %v, 权限: %v", parentDir, parentStat.IsDir(), parentStat.Mode())
		}
	}

	// 详细检查 PathExist
	log.Printf("--- 检查 PathExist ---")
	if exists := pkg.PathExist(op.path); exists {
		log.Printf("✓ 路径已存在: %q", op.path)
		if stat, err := os.Stat(op.path); err == nil {
			log.Printf("  路径信息: 是目录=%v, 权限=%v, 大小=%d", stat.IsDir(), stat.Mode(), stat.Size())
		}
	} else {
		log.Printf("✗ 路径不存在: %q", op.path)
		log.Printf("--- 尝试创建目录 ---")
		log.Printf("调用 PathCreate: %q", op.path)

		// 在创建前再次确认父目录
		if !filepath.IsAbs(op.path) {
			// 相对路径，尝试在当前目录创建
			log.Printf("使用相对路径创建，当前工作目录: %q", wd)
		}

		err := pkg.PathCreate(op.path)
		if err != nil {
			log.Printf("✗ PathCreate 失败: %v", err)
			log.Printf("错误类型: %T", err)
			log.Printf("尝试获取更详细的错误信息...")

			// 尝试手动创建看看具体错误
			if mkdirErr := os.MkdirAll(op.path, 0755); mkdirErr != nil {
				log.Printf("直接调用 os.MkdirAll 也失败: %v", mkdirErr)
			} else {
				log.Printf("✓ 直接调用 os.MkdirAll 成功!")
			}

			// 检查环境变量和权限
			log.Printf("环境变量检查:")
			log.Printf("  USER: %s", os.Getenv("USER"))
			log.Printf("  HOME: %s", os.Getenv("HOME"))

			// 检查当前用户
			log.Printf("当前用户信息:")
			log.Printf("  用户ID: %d", os.Getuid())
			log.Printf("  组ID: %d", os.Getgid())

			// 尝试创建测试路径
			testPath := "/tmp/test-logger-dir"
			if testErr := os.MkdirAll(testPath, 0755); testErr != nil {
				log.Printf("⚠️ 无法在 /tmp 创建测试目录: %v", testErr)
			} else {
				log.Printf("✓ 可以在 /tmp 创建目录，权限正常")
				os.Remove(testPath)
			}

			log.Fatalf("create dir error: %s", err.Error())
		} else {
			log.Printf("✓ PathCreate 成功: %q", op.path)
			// 验证创建结果
			if stat, err := os.Stat(op.path); err == nil {
				log.Printf("  验证: 路径已创建，是目录=%v, 权限=%v", stat.IsDir(), stat.Mode())
			} else {
				log.Printf("  ⚠️ 警告: 创建成功但无法 stat: %v", err)
			}
		}
	}
	log.Printf("=== SetupLogger 目录检查完成 ===")

	var outputErr error
	var output io.Writer
	log.Printf("--- 配置日志输出 ---")
	switch op.stdout {
	case "file":
		log.Printf("使用文件输出模式: %q", op.path)
		output, outputErr = writer.NewFileWriter(
			writer.WithPath(op.path),
			writer.WithCap(op.cap<<10),
		)
		if outputErr != nil {
			log.Printf("✗ NewFileWriter 失败: %v", outputErr)
			log.Fatalf("logger setup error: %s", outputErr.Error())
		}
		log.Printf("✓ NewFileWriter 成功")
	default:
		log.Printf("使用标准输出模式 (stdout)")
		output = os.Stdout
	}

	log.Printf("--- 解析日志级别 ---")
	var level logger.Level
	var levelErr error
	level, levelErr = logger.GetLevel(op.level)
	if levelErr != nil {
		log.Printf("✗ 解析日志级别失败: %v", levelErr)
		log.Fatalf("get logger level error, %s", levelErr.Error())
	}
	log.Printf("✓ 日志级别: %v", level)

	log.Printf("--- 初始化 Logger ---")
	switch op.driver {
	case "zap":
		log.Printf("使用 zap driver")
		var zapErr error
		logger.DefaultLogger, zapErr = zap.NewLogger(logger.WithLevel(level), zap.WithOutput(output), zap.WithCallerSkip(2))
		if zapErr != nil {
			log.Printf("✗ zap.NewLogger 失败: %v", zapErr)
			log.Fatalf("new zap logger error: %s", zapErr.Error())
		}
		log.Printf("✓ zap.Logger 初始化成功")
	//case "logrus":
	//	setLogger = logrus.NewLogger(logger.WithLevel(level), logger.WithOutput(output), logrus.ReportCaller())
	default:
		log.Printf("使用 default driver")
		logger.DefaultLogger = logger.NewLogger(logger.WithLevel(level), logger.WithOutput(output))
		log.Printf("✓ default Logger 初始化成功")
	}
	log.Printf("=== SetupLogger 完成 ===")
	return logger.DefaultLogger
}
