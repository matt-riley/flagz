package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
)

const (
	EventTypeUpdated    = "updated"
	EventTypeDeleted    = "deleted"
	bestEffortTimeout   = 2 * time.Second
	cacheResyncInterval = time.Minute
	cacheReloadTimeout  = 5 * time.Second
)

var (
	ErrFlagNotFound    = errors.New("flag not found")
	ErrInvalidRules    = errors.New("invalid rules")
	ErrInvalidVariants = errors.New("invalid variants")
)

type Repository interface {
	CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	GetFlag(ctx context.Context, key string) (repository.Flag, error)
	ListFlags(ctx context.Context) ([]repository.Flag, error)
	DeleteFlag(ctx context.Context, key string) error
	ListEventsSince(ctx context.Context, eventID int64) ([]repository.FlagEvent, error)
	ListEventsSinceForKey(ctx context.Context, eventID int64, key string) ([]repository.FlagEvent, error)
	PublishFlagEvent(ctx context.Context, event repository.FlagEvent) (repository.FlagEvent, error)
}

type cacheInvalidationSubscriber interface {
	SubscribeFlagInvalidation(ctx context.Context) (<-chan struct{}, error)
}

type ResolveRequest struct {
	Key          string
	Context      core.EvaluationContext
	DefaultValue bool
}

type ResolveResult struct {
	Key   string `json:"key"`
	Value bool   `json:"value"`
}

type Service struct {
	repo  Repository
	mu    sync.RWMutex
	cache map[string]repository.Flag
}

func New(ctx context.Context, repo Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}

	svc := &Service{
		repo:  repo,
		cache: make(map[string]repository.Flag),
	}

	if err := svc.LoadCache(ctx); err != nil {
		return nil, err
	}
	if subscriber, ok := repo.(cacheInvalidationSubscriber); ok {
		if err := svc.startCacheInvalidationListener(ctx, subscriber); err != nil {
			return nil, err
		}
	}

	return svc, nil
}

func (s *Service) LoadCache(ctx context.Context) error {
	flags, err := s.repo.ListFlags(ctx)
	if err != nil {
		return fmt.Errorf("load flags: %w", err)
	}

	next := make(map[string]repository.Flag, len(flags))
	for _, flag := range flags {
		next[flag.Key] = flag
	}

	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()

	return nil
}

func (s *Service) CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if strings.TrimSpace(flag.Key) == "" {
		return repository.Flag{}, errors.New("flag key is required")
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

func (s *Service) UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if strings.TrimSpace(flag.Key) == "" {
		return repository.Flag{}, errors.New("flag key is required")
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
			s.deleteCachedFlag(flag.Key)
			return repository.Flag{}, ErrFlagNotFound
		}
		return repository.Flag{}, fmt.Errorf("update flag: %w", err)
	}

	s.setCachedFlag(updated)
	s.publishFlagEventBestEffort(ctx, EventTypeUpdated, updated)

	return updated, nil
}

func (s *Service) GetFlag(ctx context.Context, key string) (repository.Flag, error) {
	if strings.TrimSpace(key) == "" {
		return repository.Flag{}, errors.New("flag key is required")
	}

	if flag, ok := s.getCachedFlag(key); ok {
		return flag, nil
	}

	flag, err := s.repo.GetFlag(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repository.Flag{}, ErrFlagNotFound
		}
		return repository.Flag{}, fmt.Errorf("get flag: %w", err)
	}

	s.setCachedFlag(flag)
	return flag, nil
}

func (s *Service) ListFlags(_ context.Context) ([]repository.Flag, error) {
	s.mu.RLock()
	flags := make([]repository.Flag, 0, len(s.cache))
	for _, flag := range s.cache {
		flags = append(flags, flag)
	}
	s.mu.RUnlock()

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Key < flags[j].Key
	})

	return flags, nil
}

func (s *Service) DeleteFlag(ctx context.Context, key string) error {
	existing, err := s.GetFlag(ctx, key)
	if err != nil {
		return err
	}

	if err := s.repo.DeleteFlag(ctx, key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.deleteCachedFlag(key)
			return ErrFlagNotFound
		}
		return fmt.Errorf("delete flag: %w", err)
	}

	s.deleteCachedFlag(key)
	s.publishFlagEventBestEffort(ctx, EventTypeDeleted, existing)

	return nil
}

func (s *Service) ResolveBoolean(ctx context.Context, key string, evalContext core.EvaluationContext, defaultValue bool) (bool, error) {
	flag, err := s.GetFlag(ctx, key)
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

func (s *Service) ResolveBatch(ctx context.Context, requests []ResolveRequest) ([]ResolveResult, error) {
	results := make([]ResolveResult, 0, len(requests))
	for _, request := range requests {
		value, err := s.ResolveBoolean(ctx, request.Key, request.Context, request.DefaultValue)
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

func (s *Service) ListEventsSince(ctx context.Context, eventID int64) ([]repository.FlagEvent, error) {
	events, err := s.repo.ListEventsSince(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list events since %d: %w", eventID, err)
	}

	return events, nil
}

func (s *Service) ListEventsSinceForKey(ctx context.Context, eventID int64, key string) ([]repository.FlagEvent, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("flag key is required")
	}

	events, err := s.repo.ListEventsSinceForKey(ctx, eventID, key)
	if err != nil {
		return nil, fmt.Errorf("list events since %d for key %q: %w", eventID, key, err)
	}

	return events, nil
}

func (s *Service) getCachedFlag(key string) (repository.Flag, bool) {
	s.mu.RLock()
	flag, ok := s.cache[key]
	s.mu.RUnlock()

	return flag, ok
}

func (s *Service) setCachedFlag(flag repository.Flag) {
	s.mu.Lock()
	s.cache[flag.Key] = flag
	s.mu.Unlock()
}

func (s *Service) deleteCachedFlag(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
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
					}
				}
				s.reloadCache(ctx)
			case _, ok := <-invalidations:
				if !ok {
					next, err := subscriber.SubscribeFlagInvalidation(ctx)
					if err != nil {
						invalidations = nil
						continue
					}
					invalidations = next
					continue
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
	_ = s.LoadCache(reloadCtx)
}

func (s *Service) publishFlagEvent(ctx context.Context, eventType string, flag repository.Flag) error {
	payload, err := json.Marshal(flag)
	if err != nil {
		return fmt.Errorf("marshal %s event payload: %w", eventType, err)
	}

	_, err = s.repo.PublishFlagEvent(ctx, repository.FlagEvent{
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
