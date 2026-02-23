// Package main is the entry point for the flagz server.
//
// The bootstrap sequence is:
//  1. Load configuration from environment variables.
//  2. Connect to PostgreSQL via pgxpool.
//  3. Create the repository and service (eagerly loading the flag cache).
//  4. Wire up the API key token validator.
//  5. Start the HTTP server (:8080) and gRPC server (:9090) concurrently.
//  6. Wait for SIGINT/SIGTERM, then gracefully shut down both servers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	flagspb "github.com/matt-riley/flagz/api/proto/v1"
	"github.com/matt-riley/flagz/internal/admin"
	"github.com/matt-riley/flagz/internal/config"
	"github.com/matt-riley/flagz/internal/logging"
	"github.com/matt-riley/flagz/internal/metrics"
	"github.com/matt-riley/flagz/internal/middleware"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/server"
	"github.com/matt-riley/flagz/internal/service"
	"github.com/matt-riley/flagz/internal/tracing"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"tailscale.com/tsnet"
)

const (
	shutdownTimeout       = 10 * time.Second
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpIdleTimeout       = 2 * time.Minute
)

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// ... (no change needed here, just context)
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.New(cfg.LogLevel)
	slog.SetDefault(log)

	shutdownTracer, err := tracing.Init(context.Background())
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracer(ctx); err != nil {
			log.Error("tracer shutdown error", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	repo := repository.NewPostgresRepository(pool, repository.WithEventBatchSize(cfg.EventBatchSize))
	m := metrics.New()
	svc, err := service.New(ctx, repo,
		service.WithLogger(log),
		service.WithCacheMetrics(m.IncCacheLoads, m.IncCacheInvalidations, m.ResetCacheSize, m.SetCacheSize),
		service.WithCacheResyncInterval(cfg.CacheResyncInterval),
	)
	if err != nil {
		return fmt.Errorf("init service: %w", err)
	}

	authFailure := middleware.WithOnAuthFailure(func() { m.AuthFailuresTotal.Inc() })
	tokenValidator := &apiKeyTokenValidator{lookup: repo}
	apiHandler := server.NewHTTPHandlerWithOptions(svc, cfg.StreamPollInterval, m, server.WithMaxJSONBodySize(cfg.MaxJSONBodySize))
	httpHandler := newHTTPHandler(apiHandler, tokenValidator, authFailure)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           otelhttp.NewHandler(httpHandler, "flagz-http"),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		IdleTimeout:       httpIdleTimeout,
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			middleware.UnaryBearerAuthInterceptor(tokenValidator, authFailure),
			m.UnaryServerInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamBearerAuthInterceptor(tokenValidator, authFailure),
			m.StreamServerInterceptor(),
		),
	)
	flagspb.RegisterFlagServiceServer(grpcServer, server.NewGRPCServerWithOptions(svc, cfg.StreamPollInterval, m))

	// Periodically update DB pool metrics.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stat := pool.Stat()
				m.SetDBPoolStats(metrics.DBPoolStats{
					Acquired: float64(stat.AcquiredConns()),
					Idle:     float64(stat.IdleConns()),
					Total:    float64(stat.TotalConns()),
				})
			}
		}
	}()

	// -------------------------------------------------------------------------
	// Admin Portal (Tailscale)
	// -------------------------------------------------------------------------
	var tsServer *tsnet.Server
	var adminLis net.Listener

	if cfg.AdminHostname != "" {
		if cfg.TSAuthKey == "" {
			return errors.New("ADMIN_HOSTNAME is set but TS_AUTH_KEY is missing")
		}

		dir := cfg.TSStateDir
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create ts-state dir: %w", err)
		}

		tsServer = &tsnet.Server{
			Hostname: cfg.AdminHostname,
			AuthKey:  cfg.TSAuthKey,
			Dir:      dir,
			Logf:     func(format string, args ...any) { log.Debug(fmt.Sprintf(format, args...), "component", "tailscale") },
		}

		// Create admin session manager
		sessionMgr := admin.NewSessionManager(ctx, repo, cfg.SessionSecret)

		// Create admin handler
		adminHandler := admin.NewHandler(repo, svc, sessionMgr, cfg.AdminHostname, log)

		// Listen on tailnet
		var err error
		adminLis, err = tsServer.Listen("tcp", ":80") // Standard HTTP port on tailnet IP
		if err != nil {
			return fmt.Errorf("listen tailnet: %w", err)
		}
		log.Info("admin portal listening", "hostname", cfg.AdminHostname, "transport", "tailscale")

		adminServer := &http.Server{Handler: adminHandler}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := adminServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("admin server shutdown error", "error", err)
			}
		}()
		go func() {
			if err := adminServer.Serve(adminLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("admin server error", "error", err)
			}
		}()
	}

	httpListener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP %s: %w", cfg.HTTPAddr, err)
	}
	defer httpListener.Close()

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", cfg.GRPCAddr, err)
	}
	defer grpcListener.Close()

	serveErrCh := make(chan error, 2)
	go func() {
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- fmt.Errorf("serve HTTP: %w", err)
		}
	}()
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			serveErrCh <- fmt.Errorf("serve gRPC: %w", err)
		}
	}()

	log.Info("server started", "http_addr", cfg.HTTPAddr, "grpc_addr", cfg.GRPCAddr)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveErrCh:
	}
	stop()

	log.Info("server shutting down")

	httpShutdownCtx, cancelHTTP := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelHTTP()
	if err := httpServer.Shutdown(httpShutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		if serveErr != nil {
			return serveErr
		}
		return fmt.Errorf("shutdown HTTP: %w", err)
	}

	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		grpcServer.Stop()
	}

	if tsServer != nil {
		tsServer.Close()
	}

	return serveErr
}

func newHTTPHandler(apiHandler http.Handler, tokenValidator middleware.TokenValidator, opts ...middleware.AuthOption) http.Handler {
	protectedAPIHandler := middleware.HTTPBearerAuthMiddleware(tokenValidator, opts...)(apiHandler)

	mux := http.NewServeMux()
	mux.Handle("/v1/", protectedAPIHandler)
	mux.Handle("GET /healthz", apiHandler)
	mux.Handle("GET /metrics", apiHandler)

	return mux
}

type apiKeyHashLookup interface {
	ValidateAPIKey(ctx context.Context, id string) (string, string, error)
}

type apiKeyTokenValidator struct {
	lookup apiKeyHashLookup
}

func (v *apiKeyTokenValidator) ValidateToken(ctx context.Context, token string) (string, error) {
	if v == nil || v.lookup == nil {
		return "", errors.New("api key validator is nil")
	}

	keyID, rawSecret, found := strings.Cut(token, ".")
	if !found || strings.TrimSpace(keyID) == "" || rawSecret == "" {
		return "", errors.New("invalid token format")
	}

	keyHash, projectID, err := v.lookup.ValidateAPIKey(ctx, keyID)
	if err != nil {
		return "", fmt.Errorf("lookup key hash: %w", err)
	}
	if !middleware.APIKeyMatchesHash(keyHash, rawSecret) {
		return "", errors.New("invalid token")
	}

	return projectID, nil
}
