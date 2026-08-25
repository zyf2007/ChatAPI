package logging

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
)

const (
	LayerApp         = "app"
	LayerHTTP        = "http"
	LayerAuth        = "auth"
	LayerTurn        = "turn"
	LayerTurnQuery   = "turnquery"
	LayerPending     = "pending"
	LayerUserControl = "usercontrol"
	LayerRepository  = "repository"
	LayerMigrate     = "migrate"
	LayerPlatform    = "platform"
	LayerAudit       = "audit"
	LayerIM          = "im"
)

type Config struct {
	Level              string
	Format             string
	HTTPSummaryEnabled bool
}

type Factory struct {
	root          *zap.Logger
	httpAccess    *zap.Logger
	httpFormatter HTTPAccessFormatter
}

type contextKey struct{}

func NewConfig(cfg config.Config) Config {
	return Config{
		Level:              cfg.LogLevel,
		Format:             cfg.LogFormat,
		HTTPSummaryEnabled: cfg.LogHTTPSummaryEnabled,
	}
}

func NewFactory(cfg Config) (*Factory, error) {
	zcfg := zap.NewProductionConfig()
	if strings.EqualFold(strings.TrimSpace(cfg.Format), "console") {
		zcfg.Encoding = "console"
	} else {
		zcfg.Encoding = "json"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Level), "debug") {
		zcfg.Development = true
	}
	zcfg.DisableStacktrace = true
	zcfg.EncoderConfig.TimeKey = "ts"
	zcfg.EncoderConfig.LevelKey = "level"
	zcfg.EncoderConfig.MessageKey = "msg"
	zcfg.EncoderConfig.NameKey = "logger"
	zcfg.EncoderConfig.CallerKey = "caller"
	zcfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zcfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	if err := zcfg.Level.UnmarshalText([]byte(normalizeLevel(cfg.Level))); err != nil {
		return nil, err
	}
	formatter := NewHTTPAccessFormatter(cfg.HTTPSummaryEnabled)
	logger, err := buildLogger(zcfg, cfg.HTTPSummaryEnabled && formatter.useColor)
	if err != nil {
		return nil, err
	}
	return &Factory{
		root:          logger,
		httpAccess:    logger.Named(LayerHTTP).With(zap.String("layer", LayerHTTP)),
		httpFormatter: formatter,
	}, nil
}

func buildLogger(cfg zap.Config, colorRequests bool) (*zap.Logger, error) {
	if !colorRequests {
		return cfg.Build(zap.AddCaller(), zap.AddCallerSkip(1))
	}
	output, closeOutput, err := zap.Open(cfg.OutputPaths...)
	if err != nil {
		return nil, err
	}
	errorOutput, closeErrorOutput, err := zap.Open(cfg.ErrorOutputPaths...)
	if err != nil {
		closeOutput()
		return nil, err
	}
	encoder := zapcore.NewJSONEncoder(cfg.EncoderConfig)
	if cfg.Encoding == "console" {
		encoder = zapcore.NewConsoleEncoder(cfg.EncoderConfig)
	}
	core := newRequestColorCore(encoder, zapcore.Lock(output), cfg.Level)
	if cfg.Sampling != nil {
		core = zapcore.NewSamplerWithOptions(core, time.Second, cfg.Sampling.Initial, cfg.Sampling.Thereafter)
	}
	options := []zap.Option{
		zap.ErrorOutput(errorOutput),
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	}
	if cfg.Development {
		options = append(options, zap.Development())
	}
	logger := zap.New(core, options...)
	_ = closeOutput
	_ = closeErrorOutput
	return logger, nil
}

func (f *Factory) Root() *zap.Logger {
	if f == nil || f.root == nil {
		return zap.NewNop()
	}
	return f.root
}

func (f *Factory) Layer(name string, fields ...zap.Field) *zap.Logger {
	logger := f.Root()
	name = strings.TrimSpace(name)
	if name != "" {
		logger = logger.Named(name).With(zap.String("layer", name))
	}
	if len(fields) > 0 {
		logger = logger.With(fields...)
	}
	return logger
}

func (f *Factory) LogHTTPAccess(entry HTTPAccessEntry) {
	if f == nil {
		return
	}
	_ = f.httpFormatter.WriteSummary(entry)
	logger := f.httpAccess
	if logger == nil {
		logger = f.Layer(LayerHTTP)
	}
	logger.Info(HTTPAccessMessage(),
		zap.String("request_id", strings.TrimSpace(entry.RequestID)),
		zap.String("http.method", strings.TrimSpace(entry.Method)),
		zap.String("http.path", trimOrFallback(entry.Path, "/")),
		zap.String("http.remote_addr", strings.TrimSpace(entry.Remote)),
		zap.Int("http.status_code", HTTPStatusFromRecorder(entry.Status)),
		zap.Duration("http.duration", entry.Duration),
	)
}

func (f *Factory) LogHTTPStart(entry HTTPAccessEntry) {
	if f == nil {
		return
	}
	entry.Phase = "start"
	_ = f.httpFormatter.WriteSummary(entry)
}

func (f *Factory) ForContext(ctx context.Context, layer string, fields ...zap.Field) *zap.Logger {
	logger := f.Layer(layer, fields...)
	if ctx == nil {
		return logger
	}
	if contextual, ok := FromContext(ctx); ok {
		logger = contextual.With(fields...)
	}
	if requestID, ok := RequestIDFromContext(ctx); ok {
		logger = logger.With(zap.String("request_id", requestID))
	}
	if connectionID, ok := ConnectionIDFromContext(ctx); ok {
		logger = logger.With(zap.String("connection_id", connectionID))
	}
	if requestActor, ok := actor.FromContext(ctx); ok {
		logger = logger.With(ActorFields(requestActor)...)
	}
	return logger
}

func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	if logger == nil {
		logger = zap.NewNop()
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) (*zap.Logger, bool) {
	logger, ok := ctx.Value(contextKey{}).(*zap.Logger)
	return logger, ok
}

func BindContext(base *zap.Logger, ctx context.Context, fields ...zap.Field) *zap.Logger {
	if base == nil {
		base = zap.NewNop()
	}
	logger := base
	if ctx != nil {
		if contextual, ok := FromContext(ctx); ok {
			logger = contextual
		}
		if requestID, ok := RequestIDFromContext(ctx); ok {
			logger = logger.With(zap.String("request_id", requestID))
		}
		if connectionID, ok := ConnectionIDFromContext(ctx); ok {
			logger = logger.With(zap.String("connection_id", connectionID))
		}
		if requestActor, ok := actor.FromContext(ctx); ok {
			logger = logger.With(ActorFields(requestActor)...)
		}
	}
	if len(fields) > 0 {
		logger = logger.With(fields...)
	}
	return logger
}

func ActorFields(value actor.Actor) []zap.Field {
	fields := make([]zap.Field, 0, 6)
	if value.UserID != "" {
		fields = append(fields, zap.String("actor.user_id", value.UserID))
	}
	if value.Username != "" {
		fields = append(fields, zap.String("actor.username", value.Username))
	}
	if value.Role != "" {
		fields = append(fields, zap.String("actor.role", value.Role))
	}
	if value.Source != "" {
		fields = append(fields, zap.String("actor.source", value.Source))
	}
	if value.PrincipalID != "" {
		fields = append(fields, zap.String("actor.principal_id", value.PrincipalID))
	}
	if value.EntryPoint != "" {
		fields = append(fields, zap.String("actor.entry_point", value.EntryPoint))
	}
	return fields
}

func normalizeLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug", "info", "warn", "error", "dpanic", "panic", "fatal":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "info"
	}
}
