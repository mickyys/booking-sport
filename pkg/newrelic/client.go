package newrelic

import (
	"fmt"
	"os"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/hamp/booking-sport/pkg/logger"
)

var app *newrelic.Application

type Config struct {
	LicenseKey  string `env:"NEW_RELIC_LICENSE_KEY"`
	AppName     string `env:"NEW_RELIC_APP_NAME" default:"ReservaYA-API"`
	Enabled     bool   `env:"NEW_RELIC_ENABLED" default:"true"`
	Environment string `env:"ENVIRONMENT" default:"development"`
}

func Init(cfg Config) (*newrelic.Application, error) {
	log := logger.GetLogger()

	if cfg.LicenseKey == "" {
		log.Warnw("new_relic_license_key not configured, New Relic disabled")
		return nil, nil
	}

	if !cfg.Enabled {
		log.Infow("new_relic disabled by configuration")
		return nil, nil
	}

	options := []newrelic.ConfigOption{
		newrelic.ConfigLicense(cfg.LicenseKey),
		newrelic.ConfigAppName(cfg.AppName),
		newrelic.ConfigDistributedTracerEnabled(true),
		newrelic.ConfigCustomInsightsEventsMaxSamplesStored(1000),
	}

	if cfg.Environment == "development" {
		options = append(options, newrelic.ConfigEnabled(false))
	}

	nrApp, err := newrelic.NewApplication(options...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize new relic: %w", err)
	}

	app = nrApp

	log.Infow("new_relic initialized",
		"app_name", cfg.AppName,
		"environment", cfg.Environment,
		"license_key", logger.MaskAPIKey(cfg.LicenseKey),
	)

	return nrApp, nil
}

func GetApp() *newrelic.Application {
	return app
}

func StartTransaction(name string, txnType string) *newrelic.Transaction {
	if app == nil {
		return nil
	}

	txn := app.StartTransaction(name)

	return txn
}

func NoticeError(txn *newrelic.Transaction, err error) {
	if txn == nil || err == nil {
		return
	}
	txn.NoticeError(err)
}

func AddAttribute(txn *newrelic.Transaction, key string, value interface{}) {
	if txn == nil {
		return
	}
	txn.AddAttribute(key, value)
}

func StartSegment(txn *newrelic.Transaction, name string) *newrelic.Segment {
	if txn == nil {
		return nil
	}
	return txn.StartSegment(name)
}

func RecordCustomMetric(name string, value float64) {
	if app == nil {
		return
	}
	app.RecordCustomMetric(name, value)
}

func Shutdown(timeout time.Duration) {
	if app == nil {
		return
	}
	app.Shutdown(timeout)
}

func GetLicenseKeyFromEnv() string {
	return os.Getenv("NEW_RELIC_LICENSE_KEY")
}

func GetAppNameFromEnv() string {
	appName := os.Getenv("NEW_RELIC_APP_NAME")
	if appName == "" {
		appName = "ReservaYA-API"
	}
	return appName
}

func GetEnabledFromEnv() bool {
	enabled := os.Getenv("NEW_RELIC_ENABLED")
	return enabled != "false"
}
