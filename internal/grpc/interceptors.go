package grpc

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type contextKey string

const (
	ctxKeyRequestID contextKey = "request_id"
	ctxKeyUserID    contextKey = "user_id"
	ctxKeyRoles     contextKey = "roles"
)

// UserIDFromContext extracts the authenticated user ID from gRPC context.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(string)
	return id, ok
}

// RolesFromContext extracts the authenticated user roles from gRPC context.
func RolesFromContext(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(ctxKeyRoles).([]string)
	return roles, ok
}

// ContextWithAuth creates a context with authenticated user info (for testing).
func ContextWithAuth(ctx context.Context, userID string, roles []string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyRoles, roles)
	return ctx
}

var (
	tokenValidator   TokenValidator
	tokenValidatorMu sync.RWMutex
)

// TokenValidator validates JWT tokens
type TokenValidator interface {
	ValidateAccessToken(token string) (*service.Claims, error)
}

// SetTokenValidator sets the global token validator
func SetTokenValidator(validator TokenValidator) {
	tokenValidatorMu.Lock()
	defer tokenValidatorMu.Unlock()
	tokenValidator = validator
}

func getTokenValidator() TokenValidator {
	tokenValidatorMu.RLock()
	defer tokenValidatorMu.RUnlock()
	return tokenValidator
}

// DefaultDeadlineInterceptor ensures every unary RPC has a deadline. If the
// client did not set one, a server-side default is applied so slow handlers
// cannot block indefinitely.
func DefaultDeadlineInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return handler(ctx, req)
	}
}

// StreamDefaultDeadlineInterceptor ensures every streaming RPC has a deadline.
func StreamDefaultDeadlineInterceptor(timeout time.Duration) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return handler(srv, &wrappedServerStream{ServerStream: ss, ctx: ctx})
	}
}

// LoggingInterceptor logs gRPC requests and responses
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		log := logger.Get()

		// Extract metadata
		md, _ := metadata.FromIncomingContext(ctx)
		requestID := getMetadataValue(md, "x-request-id")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Extract peer info
		p, _ := peer.FromContext(ctx)
		clientIP := ""
		if p != nil {
			clientIP = p.Addr.String()
		}

		// Log request
		log.Info("gRPC request started",
			"method", info.FullMethod,
			"request_id", requestID,
			"client_ip", clientIP,
		)

		// Add request ID to context
		ctx = context.WithValue(ctx, ctxKeyRequestID, requestID)

		// Call handler
		resp, err := handler(ctx, req)

		// Calculate duration
		duration := time.Since(start)

		// Log response
		if err != nil {
			log.Error("gRPC request failed",
				"method", info.FullMethod,
				"request_id", requestID,
				"duration", duration,
				"error", err,
			)
		} else {
			log.Info("gRPC request completed",
				"method", info.FullMethod,
				"request_id", requestID,
				"duration", duration,
			)
		}

		return resp, err
	}
}

// RecoveryInterceptor recovers from panics in handlers
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				log := logger.Get()
				log.Error("Panic recovered in gRPC handler",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(debug.Stack()),
				)

				// Record error in span
				span := trace.SpanFromContext(ctx)
				if span != nil {
					span.RecordError(fmt.Errorf("panic: %v", r))
					span.SetStatus(codes.Error, "Panic occurred")
				}

				err = status.Errorf(grpccodes.Internal, "Internal server error")
			}
		}()

		return handler(ctx, req)
	}
}

// AuthInterceptor validates authentication tokens
func AuthInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Skip auth for health check
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}

		// Skip auth for public methods
		if isPublicMethod(info.FullMethod) {
			return handler(ctx, req)
		}

		// Extract token from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(grpccodes.Unauthenticated, "Missing metadata")
		}

		token := getAuthToken(md)
		if token == "" {
			return nil, status.Error(grpccodes.Unauthenticated, "Missing authentication token")
		}

		// Validate token (this should call your JWT validation logic)
		userID, roles, err := validateToken(token)
		if err != nil {
			return nil, status.Error(grpccodes.Unauthenticated, "Invalid token")
		}

		// Add user info to context
		ctx = context.WithValue(ctx, ctxKeyUserID, userID)
		ctx = context.WithValue(ctx, ctxKeyRoles, roles)

		// Add to span attributes
		span := trace.SpanFromContext(ctx)
		if span != nil {
			span.SetAttributes(
				attribute.String("user.id", userID),
				attribute.StringSlice("user.roles", roles),
			)
		}

		return handler(ctx, req)
	}
}

// MetricsInterceptor records metrics for gRPC calls
func MetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Call handler
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(start)
		statusCode := grpccodes.OK
		if err != nil {
			statusCode = status.Code(err)
		}

		recordGRPCMetrics(info.FullMethod, statusCode, duration)

		return resp, err
	}
}

// perClientEntry holds a rate limiter and its last access time.
type perClientEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// perClientLimiter manages per-client rate limiters with automatic eviction
// of stale entries to prevent unbounded memory growth.
type perClientLimiter struct {
	mu       sync.Mutex
	limiters map[string]*perClientEntry
	rps      rate.Limit
	burst    int
}

const limiterEvictInterval = 5 * time.Minute
const limiterEntryTTL = 10 * time.Minute

func newPerClientLimiter(rps float64, burst int) *perClientLimiter {
	pcl := &perClientLimiter{
		limiters: make(map[string]*perClientEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go pcl.evictLoop()
	return pcl
}

func (l *perClientLimiter) getLimiter(clientIP string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.limiters[clientIP]
	if !ok {
		entry = &perClientEntry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.limiters[clientIP] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (l *perClientLimiter) evictLoop() {
	ticker := time.NewTicker(limiterEvictInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.evictStale()
	}
}

func (l *perClientLimiter) evictStale() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-limiterEntryTTL)
	for ip, entry := range l.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(l.limiters, ip)
		}
	}
}

// RateLimitInterceptor implements per-client rate limiting with the given
// requests-per-second rate and burst size.
func RateLimitInterceptor(rps float64, burst int) grpc.UnaryServerInterceptor {
	pcl := newPerClientLimiter(rps, burst)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip rate limiting for health checks
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}

		clientIP := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			clientIP = p.Addr.String()
		}

		if !pcl.getLimiter(clientIP).Allow() {
			return nil, status.Error(grpccodes.ResourceExhausted, "Rate limit exceeded")
		}

		return handler(ctx, req)
	}
}

// Stream interceptors

// StreamLoggingInterceptor logs streaming RPC requests
func StreamLoggingInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		log := logger.Get()

		// Extract metadata
		md, _ := metadata.FromIncomingContext(ss.Context())
		requestID := getMetadataValue(md, "x-request-id")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Log stream start
		log.Info("gRPC stream started",
			"method", info.FullMethod,
			"request_id", requestID,
			"is_client_stream", info.IsClientStream,
			"is_server_stream", info.IsServerStream,
		)

		// Propagate request ID in stream context
		ctx := context.WithValue(ss.Context(), ctxKeyRequestID, requestID)
		wrappedStream := &wrappedServerStream{ServerStream: ss, ctx: ctx}

		// Call handler
		err := handler(srv, wrappedStream)

		// Log stream end
		duration := time.Since(start)
		if err != nil {
			log.Error("gRPC stream failed",
				"method", info.FullMethod,
				"request_id", requestID,
				"duration", duration,
				"error", err,
			)
		} else {
			log.Info("gRPC stream completed",
				"method", info.FullMethod,
				"request_id", requestID,
				"duration", duration,
			)
		}

		return err
	}
}

// StreamRecoveryInterceptor recovers from panics in stream handlers
func StreamRecoveryInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{}, ss grpc.ServerStream,
		info *grpc.StreamServerInfo, handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log := logger.Get()
				log.Error("Panic recovered in gRPC stream handler",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(debug.Stack()),
				)
				err = status.Errorf(grpccodes.Internal, "Internal server error")
			}
		}()

		return handler(srv, ss)
	}
}

// wrappedServerStream wraps grpc.ServerStream to override Context().
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// StreamAuthInterceptor validates authentication for streaming RPCs
func StreamAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Skip auth for public methods
		if isPublicMethod(info.FullMethod) {
			return handler(srv, ss)
		}

		// Extract token from metadata
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(grpccodes.Unauthenticated, "Missing metadata")
		}

		token := getAuthToken(md)
		if token == "" {
			return status.Error(grpccodes.Unauthenticated, "Missing authentication token")
		}

		// Validate token
		userID, roles, err := validateToken(token)
		if err != nil {
			return status.Error(grpccodes.Unauthenticated, "Invalid token")
		}

		// Add user info to stream context
		ctx := ss.Context()
		ctx = context.WithValue(ctx, ctxKeyUserID, userID)
		ctx = context.WithValue(ctx, ctxKeyRoles, roles)

		return handler(srv, &wrappedServerStream{ServerStream: ss, ctx: ctx})
	}
}

// StreamRateLimitInterceptor implements per-client rate limiting for streaming RPCs.
func StreamRateLimitInterceptor(rps float64, burst int) grpc.StreamServerInterceptor {
	pcl := newPerClientLimiter(rps, burst)
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		clientIP := "unknown"
		if p, ok := peer.FromContext(ss.Context()); ok && p.Addr != nil {
			clientIP = p.Addr.String()
		}

		if !pcl.getLimiter(clientIP).Allow() {
			return status.Error(grpccodes.ResourceExhausted, "Rate limit exceeded")
		}

		return handler(srv, ss)
	}
}

// StreamMetricsInterceptor records metrics for streaming RPCs
func StreamMetricsInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		// Call handler
		err := handler(srv, ss)

		// Record metrics
		duration := time.Since(start)
		statusCode := grpccodes.OK
		if err != nil {
			statusCode = status.Code(err)
		}

		recordGRPCMetrics(info.FullMethod, statusCode, duration)

		return err
	}
}

// Helper functions

func getMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func getAuthToken(md metadata.MD) string {
	// Try authorization header first
	token := getMetadataValue(md, "authorization")
	if token != "" {
		// Remove "Bearer " prefix if present
		if len(token) > 7 && token[:7] == "Bearer " {
			return token[7:]
		}
		return token
	}

	// Try x-auth-token header
	return getMetadataValue(md, "x-auth-token")
}

func generateRequestID() string {
	return "grpc-" + uuid.NewString()
}

var publicMethods = map[string]struct{}{
	"/gocore.v1.AuthService/Login":                {},
	"/gocore.v1.AuthService/Register":             {},
	"/gocore.v1.AuthService/RefreshToken":         {},
	"/gocore.v1.AuthService/RequestPasswordReset": {},
	"/gocore.v1.AuthService/ResetPassword":        {},
}

func isPublicMethod(method string) bool {
	_, ok := publicMethods[method]
	return ok
}

func validateToken(token string) (userID string, roles []string, err error) {
	validator := getTokenValidator()
	if validator == nil {
		return "", nil, errors.NewUnauthorized("Token validator not initialized")
	}

	if token == "" {
		return "", nil, errors.NewUnauthorized("Invalid token")
	}

	// Validate the token
	claims, err := validator.ValidateAccessToken(token)
	if err != nil {
		return "", nil, errors.NewUnauthorized(fmt.Sprintf("Invalid token: %v", err))
	}

	return claims.UserID.String(), claims.Roles, nil
}

func recordGRPCMetrics(method string, statusCode grpccodes.Code, duration time.Duration) {
	metricsService := metrics.GetMetrics()
	if metricsService == nil {
		return
	}
	metricsService.RecordGRPCRequest(method, statusCode.String(), duration)
}

// ErrorInterceptor converts internal errors to gRPC status errors
func ErrorInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, ToGRPCError(err)
		}
		return resp, nil
	}
}

// toGRPCError converts internal errors to gRPC status errors
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Convert custom errors to gRPC errors
	switch e := err.(type) {
	case *errors.ProblemDetail:
		code := httpStatusToGRPCCode(e.Status)
		message := e.Detail
		if code == grpccodes.Internal {
			message = "Internal server error"
		}
		return status.Error(code, message)
	default:
		return status.Error(grpccodes.Internal, "Internal server error")
	}
}

func httpStatusToGRPCCode(statusCode int) grpccodes.Code {
	switch statusCode {
	case http.StatusBadRequest:
		return grpccodes.InvalidArgument
	case http.StatusUnauthorized:
		return grpccodes.Unauthenticated
	case http.StatusForbidden:
		return grpccodes.PermissionDenied
	case http.StatusNotFound:
		return grpccodes.NotFound
	case http.StatusConflict:
		return grpccodes.AlreadyExists
	case http.StatusTooManyRequests:
		return grpccodes.ResourceExhausted
	case http.StatusServiceUnavailable:
		return grpccodes.Unavailable
	default:
		return grpccodes.Internal
	}
}
