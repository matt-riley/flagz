package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	flagspb "github.com/mattriley/flagz/api/proto/v1"
	"github.com/mattriley/flagz/internal/config"
	"github.com/mattriley/flagz/internal/middleware"
	"github.com/mattriley/flagz/internal/repository"
	"github.com/mattriley/flagz/internal/server"
	"github.com/mattriley/flagz/internal/service"
	"google.golang.org/grpc"
)

const (
	shutdownTimeout       = 10 * time.Second
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpIdleTimeout       = 2 * time.Minute
)

func main() {
	if err := run(); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	repo := repository.NewPostgresRepository(pool)
	svc, err := service.New(ctx, repo)
	if err != nil {
		return fmt.Errorf("init service: %w", err)
	}

	tokenValidator := &apiKeyTokenValidator{lookup: repo}
	apiHandler := server.NewHTTPHandlerWithStreamPollInterval(svc, cfg.StreamPollInterval)
	httpHandler := newHTTPHandler(apiHandler, tokenValidator)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpHandler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		IdleTimeout:       httpIdleTimeout,
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.UnaryBearerAuthInterceptor(tokenValidator)),
		grpc.StreamInterceptor(middleware.StreamBearerAuthInterceptor(tokenValidator)),
	)
	flagspb.RegisterFlagServiceServer(grpcServer, server.NewGRPCServerWithStreamPollInterval(svc, cfg.StreamPollInterval))

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

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveErrCh:
	}
	stop()

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

	return serveErr
}

func newHTTPHandler(apiHandler http.Handler, tokenValidator middleware.TokenValidator) http.Handler {
	protectedAPIHandler := middleware.HTTPBearerAuthMiddleware(tokenValidator)(apiHandler)

	mux := http.NewServeMux()
	mux.Handle("/v1/", protectedAPIHandler)
	mux.Handle("GET /healthz", apiHandler)
	mux.Handle("GET /metrics", apiHandler)

	return mux
}

type apiKeyHashLookup interface {
	ValidateAPIKey(ctx context.Context, id string) (string, error)
}

type apiKeyTokenValidator struct {
	lookup apiKeyHashLookup
}

func (v *apiKeyTokenValidator) ValidateToken(ctx context.Context, token string) error {
	if v == nil || v.lookup == nil {
		return errors.New("api key validator is nil")
	}

	keyID, rawSecret, found := strings.Cut(token, ".")
	if !found || strings.TrimSpace(keyID) == "" || rawSecret == "" {
		return errors.New("invalid token format")
	}

	keyHash, err := v.lookup.ValidateAPIKey(ctx, keyID)
	if err != nil {
		return fmt.Errorf("lookup key hash: %w", err)
	}
	if !middleware.APIKeyMatchesHash(keyHash, rawSecret) {
		return errors.New("invalid token")
	}

	return nil
}
