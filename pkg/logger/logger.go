package logger

import (
	"io"
	"log"
	"os"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"
)

func init() {
	l, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(l)
}

func Init(options *Options) {
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())

	var level zapcore.Level
	if err := level.Set(options.Level); err != nil {
		level = zapcore.InfoLevel
	}
	var stackLevel zapcore.Level
	if err := stackLevel.Set(options.AddStackLevel); err != nil {
		stackLevel = zapcore.FatalLevel
	}

	levelEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= level
	})

	writeSyncer := zapcore.Lock(os.Stdout)
	if options.Filename != "" {
		w := &lumberjack.Logger{
			Filename:   options.Filename,
			MaxSize:    options.MaxSize,
			MaxBackups: options.MaxBackups,
			MaxAge:     options.MaxAge,
			Compress:   options.Compress,
		}
		writeSyncer = zapcore.AddSync(io.MultiWriter(os.Stdout, w))

		gormlogger.Default = gormlogger.New(log.New(writeSyncer, "\r\n", log.LstdFlags), gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		})
	}

	// Join the outputs, encoders, and level-handling functions into zapcore.Cores
	core := zapcore.NewCore(consoleEncoder, writeSyncer, levelEnabler)

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(stackLevel))
	zap.ReplaceGlobals(logger)
}
