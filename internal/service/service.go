// Package service implements the feature-flag business logic and caching layer.
//
// It sits between the transport layer (HTTP/gRPC) and the persistence layer
// (repository), owning flag CRUD, evaluation, event publishing, and an
// in-memory flag cache that is eagerly loaded on startup and kept fresh via
// PostgreSQL LISTEN/NOTIFY invalidations plus periodic resync.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
)

const (
	// EventTypeUpdated is the event type emitted when a flag is created or updated.
	EventTypeUpdated = "updated"
	// EventTypeDeleted is the event type emitted when a flag is deleted.
	EventTypeDeleted    = "deleted"
	bestEffortTimeout   = 2 * time.Second
	cacheResyncInterval = time.Minute
	cacheReloadTimeout  = 5 * time.Second
)

var (
	// ErrFlagNotFound is returned when a requested flag does not exist.
	ErrFlagNotFound = errors.New("flag not found")
	// ErrInvalidRules is returned when flag rules JSON is malformed.
	ErrInvalidRules = errors.New("invalid rules")
	// ErrInvalidVariants is returned when flag variants JSON is malformed.
	ErrInvalidVariants = errors.New("invalid variants")
	// ErrFlagKeyRequired is returned when a flag key is empty or blank.
	ErrFlagKeyRequired = errors.New("flag key is required")
	// ErrProjectIDRequired is returned when a project ID is empty or blank.
	ErrProjectIDRequired = errors.New("project ID is required")
)

// Repository defines the persistence operations required by [Service].
// It is satisfied by [repository.PostgresRepository].
type Repository interface {
	CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	GetFlag(ctx context.Context, projectID, key string) (repository.Flag, error)
	ListFlags(ctx context.Context) ([]repository.Flag, error)
	DeleteFlag(ctx context.Context, projectID, key string) error
	ListEventsSince(ctx context.Context, projectID string, eventID int64) ([]repository.FlagEvent, error)
	ListEventsSinceForKey(ctx context.Context, projectID string, eventID int64, key string) ([]repository.FlagEvent, error)
	PublishFlagEvent(ctx context.Context, event repository.FlagEvent) (repository.FlagEvent, error)
}

type cacheInvalidationSubscriber interface {
	SubscribeFlagInvalidation(ctx context.Context) (<-chan struct{}, error)
}

// ResolveRequest represents a single flag evaluation request, pairing a flag
// key with an evaluation context and a default value to fall back on.
type ResolveRequest struct {
	ProjectID    string
	Key          string
	Context      core.EvaluationContext
	DefaultValue bool
}

// ResolveResult holds the evaluated boolean result for a single flag key.
type ResolveResult struct {
	Key   string `json:"key"`
	Value bool   `json:"value"`
}

// Service is the central feature-flag service. It manages flag CRUD operations,
// boolean evaluation, event streaming, and an in-memory cache of all flags.
// All exported methods are safe for concurrent use.
type Service struct {
	repo            Repository
	log             *slog.Logger
	mu              sync.RWMutex
	cache           map[string]map[string]repository.Flag // map[projectID]map[key]Flag
	onCacheLoad     func()
	onInvalidation  func()
	onCacheUpdate   func(projectID string, size float64)
}

// Option configures optional [Service] parameters.
type Option func(*Service)

// WithLogger sets the structured logger used by [Service]. When omitted,
// [slog.Default] is used. Passing nil is a no-op and leaves the existing
// logger unchanged.
func WithLogger(log *slog.Logger) Option {
	return func(s *Service) {
		if log == nil {
			return
		}
		s.log = log
	}
}

// WithCacheMetrics registers callbacks invoked on cache operations, allowing
// Prometheus (or any other) instrumentation without importing the metrics
// package.
func WithCacheMetrics(onLoad, onInvalidation func(), onCacheUpdate func(projectID string, size float64)) Option {
	return func(s *Service) {
		s.onCacheLoad = onLoad
		s.onInvalidation = onInvalidation
		s.onCacheUpdate = onCacheUpdate
	}
}

// New creates a [Service], eagerly loading the flag cache from the repository.
// If the repository implements cache invalidation subscriptions, a background
// listener is started to keep the cache fresh.
func New(ctx context.Context, repo Repository, opts ...Option) (*Service, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}

	svc := &Service{
		repo:  repo,
		log:   slog.Default(),
		cache: make(map[string]map[string]repository.Flag),
	}
	for _, opt := range opts {
		opt(svc)
	}

	if err := svc.LoadCache(ctx); err != nil {
		return nil, err
	}
	svc.log.Info("flag cache loaded", "flags", svc.cacheSize())

	if subscriber, ok := repo.(cacheInvalidationSubscriber); ok {
		if err := svc.startCacheInvalidationListener(ctx, subscriber); err != nil {
			return nil, err
		}
		svc.log.Info("cache invalidation listener started")
	}

	return svc, nil
}

// LoadCache replaces the in-memory flag cache with a fresh snapshot from the
// repository. It is called during startup and periodically to ensure
// consistency.
func (s *Service) LoadCache(ctx context.Context) error {
	flags, err := s.repo.ListFlags(ctx)
	if err != nil {
		return fmt.Errorf("load flags: %w", err)
	}

	next := make(map[string]map[string]repository.Flag)
	for _, flag := range flags {
		if _, ok := next[flag.ProjectID]; !ok {
			next[flag.ProjectID] = make(map[string]repository.Flag)
		}
		next[flag.ProjectID][flag.Key] = flag
	}

	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()

	if s.onCacheLoad != nil {
		s.onCacheLoad()
	}
	if s.onCacheUpdate != nil {
		for pid, m := range next {
			s.onCacheUpdate(pid, float64(len(m)))
		}
	}

	return nil
}

// CreateFlag validates and persists a new flag, updates the cache, and
// publishes an "updated" event on a best-effort basis.
func (s *Service) CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if strings.TrimSpace(flag.Key) == "" {
		return repository.Flag{}, ErrFlagKeyRequired
	}
	if strings.TrimSpace(flag.ProjectID) == "" {
		return repository.Flag{}, ErrProjectIDRequired
	}
	if _, err := parseRulesJSON(flag.Rules); err != nil {
		return repository.Flag{}, err
	}
	if err := parseVariantsJSON(flag.Variants); err != nil {
		return repository.Flag{}, err
	}

	created, err := s.repo.CreateFlag(ctx, flag)
	if err != nil {
		return repository.Flag{}, fmt.Errorf("create flag: %w", err)
	}

	s.setCachedFlag(created)
	s.publishFlagEventBestEffort(ctx, EventTypeUpdated, created)

	return created, nil
}

// UpdateFlag validates and persists changes to an existing flag. Returns
// [ErrFlagNotFound] if the flag does not exist. On success, the cache is
// updated and an "updated" event is published.
func (s *Service) UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if strings.TrimSpace(flag.Key) == "" {
		return repository.Flag{}, ErrFlagKeyRequired
	}
	if strings.TrimSpace(flag.ProjectID) == "" {
		return repository.Flag{}, ErrProjectIDRequired
	}
	if _, err := parseRulesJSON(flag.Rules); err != nil {
		return repository.Flag{}, err
	}
	if err := parseVariantsJSON(flag.Variants); err != nil {
		return repository.Flag{}, err
	}

	updated, err := s.repo.UpdateFlag(ctx, flag)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.deleteCachedFlag(flag.ProjectID, flag.Key)
			return repository.Flag{}, ErrFlagNotFound
		}
		return repository.Flag{}, fmt.Errorf("update flag: %w", err)
	}

	s.setCachedFlag(updated)
	s.publishFlagEventBestEffort(ctx, EventTypeUpdated, updated)

	return updated, nil
}

// GetFlag returns a flag by projectID and key, serving from the in-memory cache when
// available and falling back to the repository. Returns [ErrFlagNotFound]
// if the flag does not exist.
func (s *Service) GetFlag(ctx context.Context, projectID, key string) (repository.Flag, error) {
	if strings.TrimSpace(key) == "" {
		return repository.Flag{}, ErrFlagKeyRequired
	}
	if strings.TrimSpace(projectID) == "" {
		return repository.Flag{}, ErrProjectIDRequired
	}

	if flag, ok := s.getCachedFlag(projectID, key); ok {
		return flag, nil
	}

	flag, err := s.repo.GetFlag(ctx, projectID, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.Flag{}, ErrFlagNotFound
		}
		return repository.Flag{}, fmt.Errorf("get flag: %w", err)
	}

	s.setCachedFlag(flag)
	return flag, nil
}

// ListFlags returns all flags for a given project from the in-memory cache, sorted by key.
func (s *Service) ListFlags(_ context.Context, projectID string) ([]repository.Flag, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectIDRequired
	}
	s.mu.RLock()
	projectFlags, ok := s.cache[projectID]
	if !ok {
		s.mu.RUnlock()
		return []repository.Flag{}, nil
	}

	flags := make([]repository.Flag, 0, len(projectFlags))
	for _, flag := range projectFlags {
		flags = append(flags, flag)
	}
	s.mu.RUnlock()

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Key < flags[j].Key
	})

	return flags, nil
}

// DeleteFlag removes a flag by projectID and key. Returns [ErrFlagNotFound] if the flag
// does not exist. On success, the cache is updated and a "deleted" event is
// published.
func (s *Service) DeleteFlag(ctx context.Context, projectID, key string) error {
	existing, err := s.GetFlag(ctx, projectID, key)
	if err != nil {
		return err
	}

	if err := s.repo.DeleteFlag(ctx, projectID, key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.deleteCachedFlag(projectID, key)
			return ErrFlagNotFound
		}
		return fmt.Errorf("delete flag: %w", err)
	}

	s.deleteCachedFlag(projectID, key)
	s.publishFlagEventBestEffort(ctx, EventTypeDeleted, existing)

	return nil
}

// ResolveBoolean evaluates a single flag against the given context and returns
// a boolean result. If the flag is not found, the provided default value is
// returned without error.
func (s *Service) ResolveBoolean(ctx context.Context, projectID, key string, evalContext core.EvaluationContext, defaultValue bool) (bool, error) {
	flag, err := s.GetFlag(ctx, projectID, key)
	if err != nil {
		if errors.Is(err, ErrFlagNotFound) {
			return defaultValue, nil
		}
		return defaultValue, err
	}

	coreFlag, err := repositoryFlagToCore(flag)
	if err != nil {
		return defaultValue, fmt.Errorf("decode flag %q rules: %w", key, err)
	}

	return core.EvaluateFlag(coreFlag, evalContext), nil
}

// ResolveBatch evaluates multiple flags in a single call, returning results
// in the same order as the requests.
func (s *Service) ResolveBatch(ctx context.Context, requests []ResolveRequest) ([]ResolveResult, error) {
	results := make([]ResolveResult, 0, len(requests))
	for _, request := range requests {
		value, err := s.ResolveBoolean(ctx, request.ProjectID, request.Key, request.Context, request.DefaultValue)
		if err != nil {
			return nil, err
		}

		results = append(results, ResolveResult{
			Key:   request.Key,
			Value: value,
		})
	}

	return results, nil
}

// ListEventsSince returns flag events with IDs greater than eventID, used by
// streaming consumers to poll for updates.
func (s *Service) ListEventsSince(ctx context.Context, projectID string, eventID int64) ([]repository.FlagEvent, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectIDRequired
	}
	events, err := s.repo.ListEventsSince(ctx, projectID, eventID)
	if err != nil {
		return nil, fmt.Errorf("list events since %d: %w", eventID, err)
	}

	return events, nil
}

// ListEventsSinceForKey returns flag events for a specific key with IDs greater
// than eventID, enabling per-flag streaming in gRPC WatchFlag.
func (s *Service) ListEventsSinceForKey(ctx context.Context, projectID string, eventID int64, key string) ([]repository.FlagEvent, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectIDRequired
	}
	if strings.TrimSpace(key) == "" {
		return nil, ErrFlagKeyRequired
	}

	events, err := s.repo.ListEventsSinceForKey(ctx, projectID, eventID, key)
	if err != nil {
		return nil, fmt.Errorf("list events since %d for key %q: %w", eventID, key, err)
	}

	return events, nil
}

func (s *Service) getCachedFlag(projectID, key string) (repository.Flag, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectFlags, ok := s.cache[projectID]; ok {
		if flag, ok := projectFlags[key]; ok {
			return flag, true
		}
	}

	return repository.Flag{}, false
}

func (s *Service) setCachedFlag(flag repository.Flag) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.cache[flag.ProjectID]; !ok {
		s.cache[flag.ProjectID] = make(map[string]repository.Flag)
	}
	s.cache[flag.ProjectID][flag.Key] = flag
}

func (s *Service) deleteCachedFlag(projectID, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if projectFlags, ok := s.cache[projectID]; ok {
		delete(projectFlags, key)
		if len(projectFlags) == 0 {
			delete(s.cache, projectID)
		}
	}
}

func (s *Service) cacheSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, m := range s.cache {
		n += len(m)
	}
	return n
}

func (s *Service) startCacheInvalidationListener(ctx context.Context, subscriber cacheInvalidationSubscriber) error {
	invalidations, err := subscriber.SubscribeFlagInvalidation(ctx)
	if err != nil {
		return fmt.Errorf("subscribe cache invalidation: %w", err)
	}

	go func() {
		resyncTicker := time.NewTicker(cacheResyncInterval)
		defer resyncTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-resyncTicker.C:
				if invalidations == nil {
					next, err := subscriber.SubscribeFlagInvalidation(ctx)
					if err == nil {
						invalidations = next
						s.log.Info("cache invalidation resubscribed")
					} else {
						s.log.Warn("cache invalidation resubscribe failed", "error", err)
					}
				}
				s.reloadCache(ctx)
			case _, ok := <-invalidations:
				if !ok {
					next, err := subscriber.SubscribeFlagInvalidation(ctx)
					if err != nil {
						s.log.Warn("cache invalidation channel closed, resubscribe failed", "error", err)
						invalidations = nil
						continue
					}
					invalidations = next
					s.log.Info("cache invalidation resubscribed after channel close")
					continue
				}
				s.log.Debug("cache invalidation received")
				if s.onInvalidation != nil {
					s.onInvalidation()
				}
				s.reloadCache(ctx)
			}
		}
	}()

	return nil
}

func (s *Service) publishFlagEventBestEffort(ctx context.Context, eventType string, flag repository.Flag) {
	// Mutations have already committed before events are published.
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bestEffortTimeout)
	defer cancel()
	_ = s.publishFlagEvent(publishCtx, eventType, flag)
}

func (s *Service) reloadCache(ctx context.Context) {
	reloadCtx, cancel := context.WithTimeout(ctx, cacheReloadTimeout)
	defer cancel()
	if err := s.LoadCache(reloadCtx); err != nil {
		s.log.Error("cache reload failed", "error", err)
	} else {
		s.log.Debug("cache reloaded", "flags", s.cacheSize())
	}
}

func (s *Service) publishFlagEvent(ctx context.Context, eventType string, flag repository.Flag) error {
	payload, err := json.Marshal(flag)
	if err != nil {
		return fmt.Errorf("marshal %s event payload: %w", eventType, err)
	}

	_, err = s.repo.PublishFlagEvent(ctx, repository.FlagEvent{
		ProjectID: flag.ProjectID,
		FlagKey:   flag.Key,
		EventType: eventType,
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("publish %s event: %w", eventType, err)
	}

	return nil
}

func repositoryFlagToCore(flag repository.Flag) (core.Flag, error) {
	rules, err := parseRulesJSON(flag.Rules)
	if err != nil {
		return core.Flag{}, err
	}

	return core.Flag{
		Key:          flag.Key,
		Disabled:     !flag.Enabled,
		DefaultValue: parseBooleanDefaultFromVariants(flag.Variants),
		Rules:        rules,
	}, nil
}

func parseRulesJSON(payload json.RawMessage) ([]core.Rule, error) {
	rules := make([]core.Rule, 0)
	if len(payload) == 0 {
		return rules, nil
	}

	if err := json.Unmarshal(payload, &rules); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRules, err)
	}

	return rules, nil
}

func parseVariantsJSON(payload json.RawMessage) error {
	if len(payload) == 0 {
		return nil
	}

	var variants any
	if err := json.Unmarshal(payload, &variants); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidVariants, err)
	}

	return nil
}

func parseBooleanDefaultFromVariants(payload json.RawMessage) *bool {
	if len(payload) == 0 {
		return nil
	}

	var variants map[string]any
	if err := json.Unmarshal(payload, &variants); err != nil {
		return nil
	}

	defaultValue, ok := variants["default"].(bool)
	if !ok {
		return nil
	}

	return &defaultValue
}
