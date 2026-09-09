package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sync/atomic"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	messagingDomain "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"github.com/mr-kaynak/go-core/internal/test/sqliteschema"
	"go.opentelemetry.io/otel"
	"go.uber.org/goleak"
	"gorm.io/gorm"
)

// failingModule aborts registration to drive New's failure path.
type failingModule struct{}

func (m *failingModule) Name() string                    { return "exploder" }
func (m *failingModule) Permissions() []Permission       { return nil }
func (m *failingModule) Register(_ *ModuleContext) error { return errors.New("boom") }

// okModule registers one public route to prove the app serves module routes.
type okModule struct{}

func (m *okModule) Name() string              { return "pinger" }
func (m *okModule) Permissions() []Permission { return nil }
func (m *okModule) Register(mctx *ModuleContext) error {
	mctx.Router.Get("/modping", func(c fiber.Ctx) error { return c.SendString("pong") })
	return nil
}

func testConfig() *Config {
	return &Config{
		App: config.AppConfig{
			Name: "app-test", Env: "test", Version: "0.0.1", BodyLimit: 4 * 1024 * 1024,
		},
		JWT: config.JWTConfig{
			Secret:        "test-secret-key-minimum-32-chars-ok",
			RefreshSecret: "test-refresh-secret-32-chars-ok!",
			Issuer:        "app-test",
			Expiry:        15 * time.Minute,
			RefreshExpiry: time.Hour,
		},
		Security:  config.SecurityConfig{BCryptCost: 4, EncryptionKey: "test-encryption-key-min-32-chars!!"},
		RateLimit: config.RateLimitConfig{PerMinute: 60},
		CORS:      config.CORSConfig{AllowedOrigins: []string{"http://localhost:3000"}},
		Email:     config.EmailConfig{SMTPHost: "localhost", SMTPPort: 1, FromEmail: "noreply@example.com"},
		Metrics:   config.MetricsConfig{Port: 0},
		Blog:      config.BlogConfig{PostsPerPage: 20, ReadTimeWPM: 200},
		OTEL:      config.OTELConfig{SampleRate: 1.0},
		Database:  config.DatabaseConfig{AutoMigrate: false},
		RabbitMQ: config.RabbitMQConfig{
			URL: "amqp://guest:guest@127.0.0.1:1/", Exchange: "app-test", QueuePrefix: "app-test",
			OutboxBatchSize: 10, OutboxMaxRetry: 3,
		},
	}
}

var testDBSeq atomic.Int64

var tracingOwnershipTestOnce atomic.Bool

// leakChecks ignores fasthttp's unstoppable package-global date-ticker
// goroutine (started lazily on the first served request, by design never
// stopped) — it is not an App-owned resource.
var leakChecks = []goleak.Option{
	goleak.IgnoreAnyFunction("github.com/valyala/fasthttp.updateServerDate.func1"),
	goleak.IgnoreAnyFunction("github.com/gofiber/fiber/v3/middleware/logger.sharedTimestamp.func1"),
	goleak.IgnoreAnyFunction("github.com/gofiber/fiber/v3/internal/memory.New.StartTimeStampUpdater.func1.1"),
	goleak.IgnoreAnyFunction("github.com/gofiber/utils/v2.StartTimeStampUpdater.func1"),
	goleak.IgnoreAnyFunction("github.com/gofiber/utils/v2.StartTimeStampUpdater.func1.1"),
	// Fiber's in-memory rate-limiter storage GC — created per storage with no
	// stop API; owned by fiber's middleware, not by the App lifecycle.
	goleak.IgnoreAnyFunction("github.com/gofiber/fiber/v3/internal/memory.(*Storage).gc"),
}

// testInfra wires the constructor seam onto SQLite + test doubles so New's
// deep paths (including module Register failure) run without PostgreSQL.
func testInfra(t *testing.T) infra {
	t.Helper()
	return infra{
		initLogger: func(_ *Config) error { return nil }, // process logger already initialized
		openDatabase: func(_ *Config) (*database.DB, error) {
			dsn := fmt.Sprintf("file:app_%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), testDBSeq.Add(1))
			gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			if err != nil {
				return nil, err
			}
			sqlDB, _ := gdb.DB()
			sqlDB.SetMaxOpenConns(1)
			if err := sqliteschema.ApplyIdentity(gdb); err != nil {
				return nil, err
			}
			if err := gdb.AutoMigrate(&messagingDomain.OutboxMessage{}, &messagingDomain.OutboxProcessingLog{}); err != nil {
				return nil, err
			}
			return &database.DB{DB: gdb}, nil
		},
		runMigrations: func(_ *database.DB) error { return nil },
		newCasbin: func(_ *Config, _ *database.DB) (*authorization.CasbinService, error) {
			return authorization.NewTestCasbinService()
		},
		newRedis:  func(_ *Config) (*cache.RedisClient, error) { return nil, nil },
		newOutbox: nil,
		newRabbit: func(cfg *Config, db *database.DB, signal <-chan struct{}) (*rabbitmq.RabbitMQService, error) {
			return rabbitmq.NewRabbitMQService(cfg, messagingRepo.NewOutboxRepository(db.DB), signal)
		},
		runBootstrap: nil,
		newTracing:   nil,
		startCleanup: false,
	}
}

func TestNew_ModuleRegisterFailure_CleansUpEverything(t *testing.T) {
	defer goleak.VerifyNone(t, append([]goleak.Option{goleak.IgnoreCurrent()}, leakChecks...)...)

	var capturedDB *database.DB
	deps := testInfra(t)
	open := deps.openDatabase
	deps.openDatabase = func(cfg *Config) (*database.DB, error) {
		db, err := open(cfg)
		capturedDB = db
		return db, err
	}

	a, err := newWithInfra(testConfig(), deps, WithModules(&failingModule{}))
	if err == nil {
		t.Fatal("expected New to fail on module Register error")
	}
	if a != nil {
		t.Fatal("failed New must not return an App")
	}
	if !strings.Contains(err.Error(), "exploder") {
		t.Fatalf("error must name the failing module: %v", err)
	}

	// The database opened during the failed construction must be closed.
	sqlDB, _ := capturedDB.DB.DB()
	if pingErr := sqlDB.Ping(); pingErr == nil {
		t.Fatal("database connection must be closed after failed New")
	}
}

func TestNew_Success_ServesModuleRoutes_ShutdownIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t, append([]goleak.Option{goleak.IgnoreCurrent()}, leakChecks...)...)

	a, err := newWithInfra(testConfig(), testInfra(t), WithModules(&okModule{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modping", nil)
	resp, err := a.FiberApp().Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("module route request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("module route: got %d, want 200", resp.StatusCode)
	}

	// Concurrent + repeated Shutdown must be race-free and idempotent.
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i] = a.Shutdown(ctx) }(i)
	}
	wg.Wait()
	for i := 1; i < 4; i++ {
		if !errors.Is(results[i], results[0]) && results[i] != results[0] {
			t.Fatalf("shutdown results diverge: %v vs %v", results[0], results[i])
		}
	}
	if err := a.Shutdown(ctx); err != results[0] {
		t.Fatalf("late Shutdown must return the first result, got %v want %v", err, results[0])
	}
}

// TestParallelLifecycles_FailedFirstOwnerNeverPublishesGlobalTracing covers
// the process-global ownership rules: the app that FAILS construction (and
// would have been the first global tracing owner) never publishes; the
// surviving app becomes the owner, keeps logging, and its Shutdown is
// flush-only — a global-provider consumer can still record spans afterwards.
func TestParallelLifecycles_FailedFirstOwnerNeverPublishesGlobalTracing(t *testing.T) {
	// Global tracing ownership is a process-wide Once by design — this test
	// can only claim it on its first execution (e.g. under -count=N).
	if !tracingOwnershipTestOnce.CompareAndSwap(false, true) {
		t.Skip("process-global tracing ownership already asserted in this process")
	}
	before := otel.GetTracerProvider()

	tracingInfra := func() infra {
		deps := testInfra(t)
		deps.newTracing = tracing.NewTracingServiceDeferred
		return deps
	}

	var wg sync.WaitGroup
	var failedErr error
	var survivor *App
	var survivorErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, failedErr = newWithInfra(testConfig(), tracingInfra(), WithModules(&failingModule{}))
	}()
	go func() {
		defer wg.Done()
		survivor, survivorErr = newWithInfra(testConfig(), tracingInfra(), WithModules(&okModule{}))
	}()
	wg.Wait()

	if failedErr == nil {
		t.Fatal("first app must fail")
	}
	if survivorErr != nil {
		t.Fatalf("survivor must construct: %v", survivorErr)
	}
	if otel.GetTracerProvider() == before {
		t.Fatal("survivor should have published the global tracer provider")
	}
	if !survivor.tracing.PublishedGlobally() {
		t.Fatal("the surviving successful app must be the global tracing owner")
	}

	// Survivor still serves and logs.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/modping", nil)
	resp, err := survivor.FiberApp().Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("survivor route: err=%v code=%d", err, resp.StatusCode)
	}

	// Flush-only proof: after the successful OWNER shuts down, a consumer of
	// the global provider can still start a recording span (the provider was
	// flushed, not stopped).
	if err := survivor.Shutdown(context.Background()); err != nil {
		t.Fatalf("survivor shutdown: %v", err)
	}
	_, span := otel.Tracer("global-consumer").Start(context.Background(), "post-owner-shutdown")
	defer span.End()
	if !span.IsRecording() {
		t.Fatal("global provider must survive its owner's Shutdown (flush-only contract)")
	}
}

func TestRun_ListenerFailureReturnsError(t *testing.T) {
	// Occupy a wildcard port so the API listener fails immediately.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := testConfig()
	cfg.App.Port = port
	cfg.Metrics.Port = 0 // admin picks an ephemeral port

	a, err := newWithInfra(cfg, testInfra(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("Run must return an error when the API listener cannot bind")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after listener failure")
	}
}
