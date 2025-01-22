package logging

import (
	"strings"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type Logger struct {
	*otelzap.Logger
}

type LoggerOption struct {
	LogLevel string
}

type Option func(o *LoggerOption)

func WithLogLevel(logLevel string) Option {
	return func(o *LoggerOption) {
		o.LogLevel = logLevel
	}
}

func NewLogger(opts ...Option) (*Logger, error) {
	option := &LoggerOption{}
	for _, opt := range opts {
		opt(option)
	}

	logger, err := makeLogger(option.LogLevel)
	if err != nil {
		return nil, err
	}
	return &Logger{Logger: logger}, nil
}

func makeLogger(logLevel string) (*otelzap.Logger, error) {
	level := zap.InfoLevel
	switch strings.ToLower(logLevel) {
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	case "fatal":
		level = zap.FatalLevel
	default:
		level = zap.InfoLevel
	}

	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(level)
	zapLogger, err := zapConfig.Build()
	if err != nil {
		return nil, err
	}

	return otelzap.New(zapLogger,
		otelzap.WithMinLevel(level),
	), nil
}
