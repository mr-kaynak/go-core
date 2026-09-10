// Package app is the public application facade of go-core: consumer projects
// depend on this package (plus the identity and coremigrations packages)
// instead of copying core sources. It owns the application lifecycle —
// configuration, database, migrations, permission registry, authorization,
// bootstrap, tracing, Redis (REDIS_MODE policy), the transactional outbox and
// the HTTP server — and registers consumer modules through the Module
// contract.
package app

import (
	"context"
	goerrors "errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/bootstrap"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cleanup"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/server"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// LoadConfig loads configuration from environment variables and defaults —
// the thin public wrapper over the internal Viper chain. Callers that use
// .env files load them first (cmd/api uses godotenv before this call).
func LoadConfig() (*Config, error) {
	return config.Load()
}

// Config is the application configuration.
//
// v0 compromise (recorded in ADR-0006): this is an alias to the internal
// config type, so all fields are effectively public. Only the variables
// documented in .env.example are supported configuration; direct struct-field
// reliance beyond them is at the consumer's own risk. A narrowed public
// config schema lands before v1.
type Config = config.Config

const defaultShutdownTimeout = 30 * time.Second

// App is a fully-wired go-core application. Construct with New, serve with
// Run, or drive the HTTP layer directly in tests via FiberApp + Shutdown.
type App struct {
	cfg *Config
	log *logger.Logger

	db             *database.DB
	redis          *cache.RedisClient
	rabbit         *rabbitmq.RabbitMQService
	outboxListener *listener.OutboxListener
	tracing        *tracing.TracingService
	srv            *server.AppServer
	registry       *authorization.PermissionRegistry
	cleanupCancel  context.CancelFunc

	shutdownOnce sync.Once
	shutdownErr  error

	// Lifecycle coordination between Run and Shutdown: "stopped" set before
	// teardown prevents listeners from ever starting afterwards; when Run has
	// started, the teardown loop keeps stopping the servers until both
	// listener goroutines have actually exited (fasthttp does not prevent a
	// Serve that begins after a Shutdown, so a single stop is not enough
	// against a pre-bind race).
	stateMu         sync.Mutex
	stopped         bool
	runStarted      bool
	listenersExited chan struct{}
}

// New wires the full application. Startup order: logger → validation →
// database → core migrations (embedded FS, honoring Database.AutoMigrate) →
// casbin → redis (REDIS_MODE policy) → outbox listener → rabbitmq → server
// construction (module permissions enter the shared registry, core routes,
// then module Register hooks) → bootstrap (module permissions are in the
// registry BEFORE bootstrap derives DB permission rows and syncs policies) →
// tracing (created deferred; published globally only at the success
// commit-point below).
//
// Error contract: if ANY step fails — a module Register hook included — every
// resource started so far is closed in reverse order and a wrapped error is
// returned. A half-initialized App never leaks, and a failed New never
// installs the process-global tracer provider.
func New(cfg *Config, opts ...Option) (*App, error) {
	return newWithInfra(cfg, defaultInfra(), opts...)
}

// infra is the constructor seam: production uses defaultInfra; lifecycle
// tests inject SQLite databases, test enforcers and no-op components to reach
// deep failure paths deterministically without PostgreSQL.
type infra struct {
	initLogger    func(cfg *Config) error
	openDatabase  func(cfg *Config) (*database.DB, error)
	prepareSchema func(ctx context.Context, cfg *Config, sources ...MigrationSource) error
	newCasbin     func(cfg *Config, db *database.DB) (*authorization.CasbinService, error)
	newRedis      func(cfg *Config) (*cache.RedisClient, error)
	newOutbox     func(cfg *Config) *listener.OutboxListener
	newRabbit     func(
		cfg *Config, db *database.DB, signal <-chan struct{},
	) (*rabbitmq.RabbitMQService, error)
	runBootstrap func(
		ctx context.Context, db *database.DB,
		casbinSvc *authorization.CasbinService, registry *authorization.PermissionRegistry,
	) error
	newTracing   func(cfg *Config) (*tracing.TracingService, error)
	startCleanup bool
}

func defaultInfra() infra {
	return infra{
		initLogger: func(cfg *Config) error {
			return logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output)
		},
		openDatabase:  database.Initialize,
		prepareSchema: PrepareSchema,
		newCasbin: func(cfg *Config, db *database.DB) (*authorization.CasbinService, error) {
			return authorization.NewCasbinService(cfg, db.DB)
		},
		newRedis: cache.NewRedisClientWithPolicy,
		newOutbox: func(cfg *Config) *listener.OutboxListener {
			l := listener.NewOutboxListener(cfg.GetDSN())
			l.Start()
			return l
		},
		newRabbit: func(cfg *Config, db *database.DB, signal <-chan struct{}) (*rabbitmq.RabbitMQService, error) {
			return rabbitmq.NewRabbitMQService(cfg, messagingRepo.NewOutboxRepository(db.DB), signal)
		},
		runBootstrap: func(
			ctx context.Context, db *database.DB,
			casbinSvc *authorization.CasbinService, registry *authorization.PermissionRegistry,
		) error {
			bs := bootstrap.NewBootstrap(db.DB, identityRepo.NewUserRepository(db.DB), casbinSvc, registry)
			if err := bs.Run(ctx); err != nil {
				return err
			}
			return bootstrap.SeedTemplates(ctx, db.DB)
		},
		newTracing:   tracing.NewTracingServiceDeferred,
		startCleanup: true,
	}
}

func newWithInfra(cfg *Config, deps infra, opts ...Option) (*App, error) {
	options := resolveOptions(opts)

	if cfg == nil {
		return nil, fmt.Errorf("app: nil config")
	}
	if cfg.App.ErrorDocsURL != "" {
		errors.SetErrorDocsURL(cfg.App.ErrorDocsURL)
	}
	if err := deps.initLogger(cfg); err != nil {
		return nil, fmt.Errorf("app: failed to initialize logger: %w", err)
	}
	validation.Init()

	a := &App{cfg: cfg, log: logger.Get(), registry: options.registry, listenersExited: make(chan struct{})}

	// From here on, any failure must release what has been started.
	fail := func(step string, err error) (*App, error) {
		if cleanupErr := a.closeResources(context.Background()); cleanupErr != nil {
			a.log.Error("Cleanup after failed startup reported an error", "error", cleanupErr)
		}
		return nil, fmt.Errorf("app: %s: %w", step, err)
	}

	db, err := deps.openDatabase(cfg)
	if err != nil {
		return fail("failed to initialize database", err)
	}
	a.db = db

	// Before anything writes. The next step builds the Casbin service, whose
	// adapter creates its own table and seeds policies, so "before bootstrap"
	// would already be too late.
	if err := deps.prepareSchema(context.Background(), cfg, allMigrationSources(options)...); err != nil {
		return fail("database schema", err)
	}

	casbinSvc, err := deps.newCasbin(cfg, db)
	if err != nil {
		return fail("failed to initialize authorization", err)
	}

	redisClient, err := deps.newRedis(cfg)
	if err != nil {
		return fail("redis startup policy", err)
	}
	a.redis = redisClient

	var signalCh <-chan struct{}
	if deps.newOutbox != nil {
		a.outboxListener = deps.newOutbox(cfg)
		if a.outboxListener != nil {
			signalCh = a.outboxListener.SignalCh()
		}
	}

	rabbit, err := deps.newRabbit(cfg, db, signalCh)
	if err != nil {
		return fail("failed to initialize messaging", err)
	}
	a.rabbit = rabbit

	// Server construction registers module permissions into the shared
	// registry, wires core routes and runs module Register hooks. It comes
	// BEFORE bootstrap so bootstrap sees module permissions in the registry.
	srv, err := server.New(cfg, db, redisClient, rabbit, casbinSvc,
		server.WithPermissionRegistry(options.registry),
		server.WithConsumerModules(options.modules...),
	)
	if err != nil {
		return fail("server construction", err)
	}
	a.srv = srv

	if deps.runBootstrap != nil {
		if err := deps.runBootstrap(context.Background(), db, casbinSvc, options.registry); err != nil {
			return fail("bootstrap", err)
		}
	}

	if deps.newTracing != nil {
		tr, err := deps.newTracing(cfg)
		if err != nil {
			// Tracing failure is non-fatal, matching existing behavior.
			a.log.Error("Failed to initialize tracing", "error", err)
		} else {
			a.tracing = tr
		}
	}

	if deps.startCleanup {
		cleanupCtx, cancel := context.WithCancel(context.Background())
		a.cleanupCancel = cancel
		go cleanup.RunIdentityCleanup(cleanupCtx, db.DB, a.log)
		go db.StartConnectionMetrics(cleanupCtx)
	}

	// SUCCESS COMMIT-POINT: only a fully-constructed App publishes its tracer
	// provider globally (first publisher wins). A failed New above never
	// reaches this line, so it can never install the global provider.
	if a.tracing != nil {
		a.tracing.PublishGlobal()
	}

	return a, nil
}

// Run starts the admin (metrics) and API listeners and blocks. Error
// contract (a deliberate fix over the old cmd/api behavior, see ADR-0006):
// a fatal error from EITHER listener triggers graceful shutdown and is
// returned; SIGINT/SIGTERM triggers graceful shutdown and returns nil.
func (a *App) Run() error {
	a.stateMu.Lock()
	if a.runStarted {
		// Checked FIRST so a repeated Run — during or after shutdown — always
		// gets the duplicate-call error and never touches lifecycle channels
		// (listenersExited is closed exclusively by the listener-wait
		// goroutine of the one real run).
		a.stateMu.Unlock()
		return fmt.Errorf("app: Run called more than once")
	}
	if a.stopped {
		// Shutdown already ran; starting listeners now would serve against
		// released resources. The check and the stopped flag share one mutex,
		// so this window is closed by construction. Nothing waits on
		// listenersExited in this path (teardown only waits when a run
		// actually started), so the channel is deliberately left untouched.
		a.stateMu.Unlock()
		return nil
	}
	a.runStarted = true
	a.stateMu.Unlock()

	// Listener completions ALWAYS land here — including the nil a listener
	// returns when a programmatic Shutdown stops it. Otherwise a consumer
	// running Run in a goroutine and calling Shutdown would leave Run blocked
	// forever on a signal that never comes.
	listenDone := make(chan error, 2)
	var listenWG sync.WaitGroup
	listenWG.Add(2)
	go func() {
		listenWG.Wait()
		close(a.listenersExited)
	}()

	go func() {
		defer listenWG.Done()
		a.log.Info("Admin server is running", "port", a.cfg.Metrics.Port)
		err := a.srv.ListenAdmin()
		if err != nil {
			err = fmt.Errorf("admin listener: %w", err)
		}
		listenDone <- err
	}()

	go func() {
		defer listenWG.Done()
		addr := fmt.Sprintf(":%d", a.cfg.App.Port)
		a.log.Info("Server is running", "address", addr)
		err := a.srv.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true})
		if err != nil {
			err = fmt.Errorf("api listener: %w", err)
		}
		listenDone <- err
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var runErr error
	select {
	case err := <-listenDone:
		if err != nil {
			a.log.Error("Listener failed", "error", err)
			runErr = err
		} else {
			a.log.Info("Listener stopped; shutting down")
		}
	case <-quit:
		a.log.Info("Shutting down server...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}

// Shutdown gracefully releases every resource the App owns, in reverse
// startup order. Idempotent and safe to call concurrently; later calls return
// the first call's result. The process-global logger and a globally-published
// tracer provider are NOT torn down here (flush only) — they are
// process-level resources owned by cmd main.
func (a *App) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		a.shutdownErr = a.closeResources(ctx)
	})
	return a.shutdownErr
}

func (a *App) closeResources(ctx context.Context) error {
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	a.stateMu.Lock()
	a.stopped = true
	runStarted := a.runStarted
	a.stateMu.Unlock()

	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}

	if a.srv != nil {
		if runStarted {
			// A single stop can lose a race against listeners that have not
			// bound yet (fasthttp allows Serve after Shutdown), so stop
			// repeatedly until BOTH listener goroutines have exited. Bounded
			// by the caller's context plus a hard cap. The FINAL attempt's
			// errors are preserved: fasthttp stops listeners before draining
			// in-flight requests, so listenersExited may already be closed
			// while the drain fails — a failed drain must still surface.
			deadline := time.After(defaultShutdownTimeout)
			var adminErr, apiErr error
		stopLoop:
			for {
				adminErr = a.srv.ShutdownAdmin()
				apiErr = a.srv.ShutdownWithContext(ctx)
				select {
				case <-a.listenersExited:
					break stopLoop
				case <-ctx.Done():
					record(fmt.Errorf("app: shutdown context expired before listeners exited: %w", ctx.Err()))
					break stopLoop
				case <-deadline:
					record(fmt.Errorf("app: listeners did not exit within the shutdown timeout"))
					break stopLoop
				case <-time.After(10 * time.Millisecond):
				}
			}
			// ErrNotRunning is the expected pre-bind outcome the retry loop
			// exists for — everything else (e.g. a drain deadline) is real.
			record(filterNotRunning(adminErr))
			record(filterNotRunning(apiErr))
		} else {
			record(a.srv.ShutdownAdmin())
			record(a.srv.ShutdownWithContext(ctx))
		}
		a.srv.StopNotifications(ctx)
		a.srv.StopSSE(ctx)
	}

	if a.rabbit != nil {
		record(a.rabbit.Close())
	}
	if a.outboxListener != nil {
		a.outboxListener.Close()
	}
	if a.redis != nil {
		record(a.redis.Close())
	}
	if a.db != nil {
		record(a.db.Close())
	}

	if a.tracing != nil {
		if a.tracing.PublishedGlobally() {
			// Global consumers (e.g. redisotel via the default provider) may
			// still trace; flush, never stop, the global provider here.
			record(a.tracing.Flush(ctx))
		} else {
			record(a.tracing.Shutdown(ctx))
		}
	}

	return firstErr
}

// filterNotRunning drops fiber's ErrNotRunning — the expected outcome of a
// stop attempt that raced ahead of the listener's bind.
func filterNotRunning(err error) error {
	if err == nil || goerrors.Is(err, fiber.ErrNotRunning) {
		return nil
	}
	return err
}

// FiberApp exposes the underlying fiber application FOR TESTING ONLY —
// httptest-style in-process requests against a constructed App. It is not a
// supported extension surface.
func (a *App) FiberApp() *fiber.App {
	return a.srv.App
}
