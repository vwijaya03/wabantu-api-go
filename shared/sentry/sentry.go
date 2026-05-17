package sentry

import (
	"encore.dev/rlog"
	"github.com/getsentry/sentry-go"
)

var secrets struct {
	SentryDSN string
}

func Init() {
	dsn := secrets.SentryDSN
	if dsn == "" {
		rlog.Info("Sentry DSN not configured, skipping")
		return
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		TracesSampleRate: 0.1,
		Environment:      "production",
	})
	if err != nil {
		rlog.Error("sentry init failed", "err", err)
	}
	rlog.Info("Sentry initialized")
}

func CaptureException(err error) {
	if err == nil {
		return
	}
	sentry.CaptureException(err)
}

func CaptureMessage(msg string) {
	sentry.CaptureMessage(msg)
}

func Flush() {
	sentry.Flush(2e9) // 2 seconds
}
