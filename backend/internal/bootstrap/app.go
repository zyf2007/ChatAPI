package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/config"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	platformbrowser "github.com/zyf2007/ChatAPI/internal/platform/browser"
	platformemail "github.com/zyf2007/ChatAPI/internal/platform/email"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/repository/audit"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/automation"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	pgrepo "github.com/zyf2007/ChatAPI/internal/repository/postgresql"
	sqliterepo "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
)

type runtimeStore interface {
	auth.Store
	chat.Store
	configrepo.Store
	automation.Store
	storage.Store
	audit.Store
	platformrepo.MaintenanceStore
}

type Options struct {
	BackendRoot string
	Mode        config.Mode
}

type notificationCloser interface {
	Close() error
}

type App struct {
	Config        config.Config
	Handler       http.Handler
	logger        *zap.Logger
	store         runtimeStore
	storeClose    func()
	notifications notificationCloser
	services      Services
	workerCancel  context.CancelFunc
	workerWG      sync.WaitGroup
	workers       []func(context.Context)
	lifecycleMu   sync.Mutex
	runStarted    bool
	closed        bool
	closeOnce     sync.Once
}

func New(ctx context.Context, options Options) (_ *App, err error) {
	backendRoot := strings.TrimSpace(options.BackendRoot)
	if backendRoot == "" {
		backendRoot, err = DetectBackendRoot()
		if err != nil {
			return nil, err
		}
	}
	mode := options.Mode
	if mode == "" {
		mode = config.ModeServe
	}
	if err := config.LoadEnv(backendRoot); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}
	cfg, err := config.FromEnv(mode, backendRoot)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	imageProcessor, err := media.NewProcessor(media.ProcessorConfig{
		URL: cfg.ImageProcessorURL, APIToken: cfg.ImageProcessorAPIToken,
		Tenant: "chatapi", Priority: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("init image processor: %w", err)
	}
	logFactory, err := logging.NewFactory(logging.NewConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}
	appLogger := logFactory.Layer(logging.LayerApp)
	store, storeClose, err := openStore(ctx, cfg, logFactory)
	if err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			storeClose()
		}
	}()

	assembled, err := assembleApplication(ctx, applicationInput{
		config: cfg, store: store, loggerFactory: logFactory, mediaProcessor: imageProcessor,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble services: %w", err)
	}
	if _, err := assembled.services.Turn.DisconnectRecoveredPending(ctx, "server restarted"); err != nil {
		_ = assembled.notifications.Close()
		return nil, fmt.Errorf("disconnect recovered pending turns: %w", err)
	}
	completed = true
	app := &App{
		Config: cfg, Handler: httprouter.New(assembled.router), logger: appLogger,
		store: store, storeClose: storeClose, notifications: assembled.notifications, services: assembled.services,
	}
	app.workers = []func(context.Context){
		func(ctx context.Context) {
			expirePendingLoop(ctx, app.services.Turn, app.services.ChatSettings, app.logger)
		},
		func(ctx context.Context) {
			if app.services.IM != nil {
				if err := app.services.IM.Run(ctx); err != nil && ctx.Err() == nil {
					app.logger.Warn("IM service stopped", zap.Error(err))
				}
			}
		},
		func(ctx context.Context) {
			storageVacuumLoop(ctx, app.Config, app.store, app.services.Audit, app.logger)
		},
	}
	if app.Config.Mode == config.ModeLab && app.Config.OpenBrowser {
		app.workers = append(app.workers, func(ctx context.Context) {
			timer := time.NewTimer(600 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = platformbrowser.Open(fmt.Sprintf("http://%s:%d", browserHost(app.Config), app.Config.Port))
			}
		})
	}
	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	workerCtx, err := a.startWorkers(ctx)
	if err != nil {
		return err
	}
	defer a.Close()

	server := &http.Server{
		Addr: fmt.Sprintf("%s:%d", a.Config.Host, a.Config.Port), Handler: a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return workerCtx },
	}
	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("http server starting")
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-workerCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("http server shutdown failed", zap.Error(err))
			if closeErr := server.Close(); closeErr != nil {
				a.logger.Warn("http server close failed", zap.Error(closeErr))
			}
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *App) startWorkers(parent context.Context) (context.Context, error) {
	if parent == nil {
		parent = context.Background()
	}
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.closed {
		return nil, errors.New("bootstrap app is closed")
	}
	if a.runStarted {
		return nil, errors.New("bootstrap app can only run once")
	}
	a.runStarted = true
	workerCtx, cancel := context.WithCancel(parent)
	a.workerCancel = cancel
	for _, worker := range a.workers {
		if worker == nil {
			continue
		}
		a.workerWG.Add(1)
		go func(run func(context.Context)) {
			defer a.workerWG.Done()
			run(workerCtx)
		}(worker)
	}
	return workerCtx, nil
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		a.lifecycleMu.Lock()
		a.closed = true
		cancel := a.workerCancel
		a.lifecycleMu.Unlock()
		if cancel != nil {
			cancel()
		}
		a.workerWG.Wait()
		if a.notifications != nil {
			if err := a.notifications.Close(); err != nil {
				a.logger.Warn("ntfy notify service close failed", zap.Error(err))
			}
		}
		if a.storeClose != nil {
			a.storeClose()
		}
	})
}

// pi-lens-ignore: go-bare-error
func ModeFromArgs(args []string) (config.Mode, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "serve" {
		return config.ModeServe, nil
	}
	if strings.TrimSpace(args[0]) == "lab" {
		return config.ModeLab, nil
	}
	return "", fmt.Errorf("unknown mode %q (expected serve or lab)", args[0])
}

// pi-lens-ignore: go-bare-error
func DetectBackendRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return DetectBackendRootFrom(wd), nil
}

func DetectBackendRootFrom(start string) string {
	current := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(start)
		}
		current = parent
	}
}

func openStore(ctx context.Context, cfg config.Config, logFactory *logging.Factory) (runtimeStore, func(), error) {
	switch strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver)) {
	case "sqlite":
		store, err := sqliterepo.Open(cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
			_ = store.Close()
			return nil, nil, fmt.Errorf("bootstrap sqlite migrations: %w", err)
		}
		return store, func() { _ = store.Close() }, nil
	case "postgres", "postgresql":
		store, err := pgrepo.Open(ctx, cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open postgresql store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := pgrepo.Bootstrap(ctx, store.Pool()); err != nil {
			store.Close()
			return nil, nil, fmt.Errorf("bootstrap postgresql migrations: %w", err)
		}
		return store, store.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database driver: %s", cfg.DatabaseDriver)
	}
}

func buildEmailSender(cfg config.Config) platformemail.Sender {
	smtpCfg := platformemail.SMTPConfigFromConfig(cfg)
	if !smtpCfg.Enabled {
		return nil
	}
	return platformemail.NewSMTPSender(smtpCfg)
}

func browserHost(cfg config.Config) string {
	host := strings.TrimSpace(cfg.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}
