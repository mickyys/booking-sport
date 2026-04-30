package logger

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrlogrus"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/sirupsen/logrus"
)

type contextKey string

const (
	TraceIDKey contextKey = "trace_id"
	SpanIDKey  contextKey = "span_id"
	UserIDKey  contextKey = "user_id"
)

type SugaredLogger struct {
	entry  *logrus.Entry
	logger *logrus.Logger
	config Config
}

var (
	globalLogger     *SugaredLogger
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

func Init(cfg Config) *SugaredLogger {
	once.Do(func() {
		globalLogger = NewLogger(cfg)
	})
	return globalLogger
}

func ReconfigureForNewRelic(cfg Config, nrApp *newrelic.Application) *SugaredLogger {
	SetNewRelicApplication(nrApp)

	logger := newLogrusLogger(cfg, nrApp)
	sugared := &SugaredLogger{
		entry: logger.WithFields(logrus.Fields{
			"service":     cfg.Service,
			"version":     cfg.Version,
			"environment": cfg.Environment,
		}),
		logger: logger,
		config: cfg,
	}

	globalLogger = sugared
	return sugared
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

func NewLogger(cfg Config) *SugaredLogger {
	newrelicAppMutex.RLock()
	nrApp := newrelicApp
	newrelicAppMutex.RUnlock()

	logger := newLogrusLogger(cfg, nrApp)
	sugared := &SugaredLogger{
		entry: logger.WithFields(logrus.Fields{
			"service":     cfg.Service,
			"version":     cfg.Version,
			"environment": cfg.Environment,
		}),
		logger: logger,
		config: cfg,
	}

	return sugared
}

func newLogrusLogger(cfg Config, nrApp *newrelic.Application) *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetLevel(parseLevel(cfg.Level))

	isJSON := !strings.EqualFold(cfg.Format, "console")

	var formatter logrus.Formatter
	if isJSON {
		formatter = &logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z0700",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyMsg:  "event",
				logrus.FieldKeyTime: "timestamp",
			},
		}
	} else {
		formatter = &logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z0700",
		}
	}

	if nrApp != nil {
		formatter = nrlogrus.NewFormatter(nrApp, formatter)
	}

	logger.SetFormatter(formatter)
	return logger
}

func parseLevel(level string) logrus.Level {
	l, err := logrus.ParseLevel(level)
	if err != nil {
		return logrus.InfoLevel
	}
	return l
}

func GetLogger() *SugaredLogger {
	if globalLogger == nil {
		globalLogger = NewLogger(Config{})
	}
	return globalLogger
}

func (l *SugaredLogger) Infow(msg string, keysAndValues ...interface{}) {
	l.entry.WithFields(fieldsFromKVs(keysAndValues...)).Info(msg)
}

func (l *SugaredLogger) Warnw(msg string, keysAndValues ...interface{}) {
	l.entry.WithFields(fieldsFromKVs(keysAndValues...)).Warn(msg)
}

func (l *SugaredLogger) Errorw(msg string, keysAndValues ...interface{}) {
	l.entry.WithFields(fieldsFromKVs(keysAndValues...)).Error(msg)
}

func (l *SugaredLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	l.entry.WithFields(fieldsFromKVs(keysAndValues...)).Fatal(msg)
}

func (l *SugaredLogger) Debugw(msg string, keysAndValues ...interface{}) {
	l.entry.WithFields(fieldsFromKVs(keysAndValues...)).Debug(msg)
}

func (l *SugaredLogger) With(fields ...interface{}) *SugaredLogger {
	return &SugaredLogger{
		entry:  l.entry.WithFields(fieldsFromKVs(fields...)),
		logger: l.logger,
		config: l.config,
	}
}

func (l *SugaredLogger) Sync() error {
	return nil
}

func fieldsFromKVs(keysAndValues ...interface{}) logrus.Fields {
	fields := logrus.Fields{}
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key, ok := keysAndValues[i].(string)
			if !ok {
				continue
			}
			fields[key] = keysAndValues[i+1]
		}
	}
	return fields
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

func FromContext(ctx context.Context) *SugaredLogger {
	logger := GetLogger()

	entry := logger.entry

	if traceID := GetTraceID(ctx); traceID != "" {
		entry = entry.WithField("trace_id", traceID)
	}
	if spanID := GetSpanID(ctx); spanID != "" {
		entry = entry.WithField("span_id", spanID)
	}
	if userID := GetUserID(ctx); userID != "" {
		entry = entry.WithField("user_id", userID)
	}

	entry = entry.WithContext(ctx)

	return &SugaredLogger{
		entry:  entry,
		logger: logger.logger,
		config: logger.config,
	}
}
