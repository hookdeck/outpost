package logging

import (
	"context"
	"strings"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type Logger struct {
	*otelzap.Logger
}

type LoggerWithCtx struct {
	*otelzap.LoggerWithCtx
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

func (l *Logger) Ctx(ctx context.Context) LoggerWithCtx {
	loggerWithCtx := l.Logger.Ctx(ctx)
	return LoggerWithCtx{LoggerWithCtx: &loggerWithCtx}
}

func (l *Logger) Audit(msg string, fields ...zap.Field) {
	l.Logger.Info(msg, append(fields, zap.Bool("audit", true))...)
}

func (l *LoggerWithCtx) Audit(msg string, fields ...zap.Field) {
	l.LoggerWithCtx.Info(msg, append(fields, zap.Bool("audit", true))...)
}
