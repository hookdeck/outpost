package logging

import (
	"context"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*otelzap.Logger
	auditLogger *otelzap.Logger
}

type LoggerWithCtx struct {
	*otelzap.LoggerWithCtx
	auditLogger *otelzap.LoggerWithCtx
}

type LoggerOption struct {
	LogLevel string
	AuditLog bool
}

type Option func(o *LoggerOption)

func WithLogLevel(logLevel string) Option {
	return func(o *LoggerOption) {
		o.LogLevel = logLevel
	}
}

func WithAuditLog(auditLog bool) Option {
	return func(o *LoggerOption) {
		o.AuditLog = auditLog
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
	var auditLogger *otelzap.Logger
	if option.AuditLog {
		auditLogger, err = makeLogger(zap.InfoLevel.String())
		if err != nil {
			return nil, err
		}
	}
	return &Logger{Logger: logger, auditLogger: auditLogger}, nil
}

func makeLogger(logLevel string) (*otelzap.Logger, error) {
	level, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		return nil, err
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
	var auditLoggerWithCtx *otelzap.LoggerWithCtx
	if l.auditLogger != nil {
		auditLoggerWithCtxValue := l.auditLogger.Ctx(ctx)
		auditLoggerWithCtx = &auditLoggerWithCtxValue
	}
	return LoggerWithCtx{LoggerWithCtx: &loggerWithCtx, auditLogger: auditLoggerWithCtx}
}

func (l *Logger) getAuditLogger() *otelzap.Logger {
	if l.auditLogger != nil {
		return l.auditLogger
	}
	return l.Logger
}

func (l *Logger) Audit(msg string, fields ...zap.Field) {
	l.getAuditLogger().Info(msg, fields...)
}

func (l *LoggerWithCtx) getAuditLogger() *otelzap.LoggerWithCtx {
	if l.auditLogger != nil {
		return l.auditLogger
	}
	return l.LoggerWithCtx
}

func (l LoggerWithCtx) Audit(msg string, fields ...zap.Field) {
	l.getAuditLogger().Info(msg, fields...)
}
