package logging

import (
	"context"
	"os"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger splits sinks by purpose: the embedded *zap.Logger handles regular
// operator logs (stdout, no OTel export) while auditLogger is otelzap-wrapped
// so Audit() lines flow to both stdout and the OTel logs SDK. This keeps
// infrastructure errors and debug noise out of customer-visible OTel sinks.
type Logger struct {
	*zap.Logger
	auditLogger *otelzap.Logger
}

type LoggerWithCtx struct {
	*zap.Logger
	ctx         context.Context
	auditLogger otelzap.LoggerWithCtx
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

	zapLogger, level, err := buildZap(option.LogLevel)
	if err != nil {
		return nil, err
	}

	auditZap, _, err := buildZap(option.LogLevel)
	if err != nil {
		return nil, err
	}
	auditLogger := otelzap.New(auditZap, otelzap.WithMinLevel(level))

	return &Logger{Logger: zapLogger, auditLogger: auditLogger}, nil
}

func buildZap(logLevel string) (*zap.Logger, zapcore.Level, error) {
	level, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		return nil, 0, err
	}

	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(level)
	zapLogger, err := zapConfig.Build()
	if err != nil {
		return nil, 0, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	zapLogger = zapLogger.With(zap.String("host.name", hostname))

	return zapLogger, level, nil
}

func (l *Logger) Ctx(ctx context.Context) LoggerWithCtx {
	return LoggerWithCtx{
		Logger:      l.Logger.With(traceFields(ctx)...),
		ctx:         ctx,
		auditLogger: l.auditLogger.Ctx(ctx),
	}
}

func (l *Logger) Audit(msg string, fields ...zap.Field) {
	l.auditLogger.Info(msg, fields...)
}

func (l LoggerWithCtx) Audit(msg string, fields ...zap.Field) {
	l.auditLogger.Info(msg, fields...)
}

func (l LoggerWithCtx) WithOptions(opts ...zap.Option) LoggerWithCtx {
	return LoggerWithCtx{
		Logger:      l.Logger.WithOptions(opts...),
		ctx:         l.ctx,
		auditLogger: l.auditLogger.WithOptions(opts...),
	}
}

// NewTestLogger wraps a zap logger for use in tests, providing an audit
// sink that points at the same zap so test output is self-contained.
func NewTestLogger(zapLogger *zap.Logger) *Logger {
	return &Logger{
		Logger:      zapLogger,
		auditLogger: otelzap.New(zapLogger, otelzap.WithMinLevel(zap.InfoLevel)),
	}
}

// traceFields extracts the active span's trace/span IDs from ctx so they
// appear on stdout logs. otelzap did this automatically; with the OTel
// log sink reserved for Audit lines we attach them manually to preserve
// log<->trace correlation in operator-facing logs.
func traceFields(ctx context.Context) []zap.Field {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}
