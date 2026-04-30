package logger

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrzap"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type contextKey string

const (
	TraceIDKey contextKey = "trace_id"
	SpanIDKey  contextKey = "span_id"
	UserIDKey  contextKey = "user_id"
)

var (
	globalLogger     *zap.SugaredLogger
	once             sync.Once
	newrelicApp      *newrelic.Application
	newrelicAppMutex sync.RWMutex
)

type Config struct {
	Level       string `env:"LOG_LEVEL" default:"info"`
	Format      string `env:"LOG_FORMAT" default:"json"`
	Service     string `env:"SERVICE_NAME" default:"booking-sport-api"`
	Version     string `env:"SERVICE_VERSION" default:"1.0.0"`
	Environment string `env:"ENVIRONMENT" default:"development"`
}

func Init(cfg Config) *zap.SugaredLogger {
	once.Do(func() {
		globalLogger = NewLogger(cfg)
	})
	return globalLogger
}

func SetNewRelicApplication(nrApp *newrelic.Application) {
	newrelicAppMutex.Lock()
	defer newrelicAppMutex.Unlock()
	newrelicApp = nrApp
}

func GetNewRelicApplication() *newrelic.Application {
	newrelicAppMutex.RLock()
	defer newrelicAppMutex.RUnlock()
	return newrelicApp
}

func NewLogger(cfg Config) *zap.SugaredLogger {
	level := parseLevel(cfg.Level)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "event",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if strings.ToLower(cfg.Format) == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	newrelicAppMutex.RLock()
	nrApp := newrelicApp
	newrelicAppMutex.RUnlock()

	if nrApp != nil {
		nrCore, err := nrzap.WrapBackgroundCore(core, nrApp)
		if err != nil {
			core = core
		} else {
			core = nrCore
		}
	}

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	sugar := logger.Sugar().With(
		"service", cfg.Service,
		"version", cfg.Version,
		"environment", cfg.Environment,
	)

	return sugar
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func GetLogger() *zap.SugaredLogger {
	if globalLogger == nil {
		globalLogger = NewLogger(Config{})
	}
	return globalLogger
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, SpanIDKey, spanID)
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func GetTraceID(ctx context.Context) string {
	if v := ctx.Value(TraceIDKey); v != nil {
		return v.(string)
	}
	return ""
}

func GetSpanID(ctx context.Context) string {
	if v := ctx.Value(SpanIDKey); v != nil {
		return v.(string)
	}
	return ""
}

func GetUserID(ctx context.Context) string {
	if v := ctx.Value(UserIDKey); v != nil {
		return v.(string)
	}
	return ""
}

func FromContext(ctx context.Context) *zap.SugaredLogger {
	logger := GetLogger()

	newrelicAppMutex.RLock()
	nrApp := newrelicApp
	newrelicAppMutex.RUnlock()

	if nrApp != nil && ctx != nil {
		txn := newrelic.FromContext(ctx)
		if txn != nil {
			core := logger.Desugar().Core()
			nrCore, err := nrzap.WrapTransactionCore(core, txn)
			if err == nil {
				nrLogger := zap.New(nrCore, zap.AddCaller(), zap.AddCallerSkip(1))
				logger = nrLogger.Sugar()
			}
		}
	}

	fields := make([]interface{}, 0)

	if traceID := GetTraceID(ctx); traceID != "" {
		fields = append(fields, "trace_id", traceID)
	}
	if spanID := GetSpanID(ctx); spanID != "" {
		fields = append(fields, "span_id", spanID)
	}
	if userID := GetUserID(ctx); userID != "" {
		fields = append(fields, "user_id", userID)
	}

	if len(fields) > 0 {
		return logger.With(fields...)
	}

	return logger
}

func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}
