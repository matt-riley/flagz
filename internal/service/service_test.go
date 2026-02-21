package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
)

func TestServiceCRUDAndEvaluation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeServiceRepository()

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	flag := repository.Flag{
		Key:         "new-ui",
		Description: "initial rollout",
		Enabled:     true,
		Rules:       json.RawMessage(`[{"attribute":"country","operator":"equals","value":"US"}]`),
		Variants:    json.RawMessage(`{}`),
	}
	if _, err := svc.CreateFlag(ctx, flag); err != nil {
		t.Fatalf("CreateFlag() error = %v", err)
	}

	got, err := svc.GetFlag(ctx, "new-ui")
	if err != nil {
		t.Fatalf("GetFlag() error = %v", err)
	}
	if got.Description != "initial rollout" {
		t.Fatalf("GetFlag().Description = %q, want %q", got.Description, "initial rollout")
	}

	value, err := svc.ResolveBoolean(ctx, "new-ui", core.EvaluationContext{
		Attributes: map[string]any{"country": "US"},
	}, false)
	if err != nil {
		t.Fatalf("ResolveBoolean() error = %v", err)
	}
	if !value {
		t.Fatalf("ResolveBoolean() = %t, want true", value)
	}

	value, err = svc.ResolveBoolean(ctx, "new-ui", core.EvaluationContext{
		Attributes: map[string]any{"country": "CA"},
	}, true)
	if err != nil {
		t.Fatalf("ResolveBoolean() error = %v", err)
	}
	if !value {
		t.Fatalf("ResolveBoolean() = %t, want true on rule mismatch fallback", value)
	}

	missing, err := svc.ResolveBoolean(ctx, "missing", core.EvaluationContext{}, true)
	if err != nil {
		t.Fatalf("ResolveBoolean(missing) error = %v", err)
	}
	if !missing {
		t.Fatalf("ResolveBoolean(missing) = %t, want true", missing)
	}

	batch, err := svc.ResolveBatch(ctx, []ResolveRequest{
		{
			Key: "new-ui",
			Context: core.EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			DefaultValue: false,
		},
		{
			Key:          "unknown",
			DefaultValue: true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveBatch() error = %v", err)
	}
	if len(batch) != 2 || !batch[0].Value || !batch[1].Value {
		t.Fatalf("ResolveBatch() = %#v, want [{new-ui true} {unknown true}]", batch)
	}

	flag.Description = "updated rollout"
	if _, err := svc.UpdateFlag(ctx, flag); err != nil {
		t.Fatalf("UpdateFlag() error = %v", err)
	}

	flags, err := svc.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags() error = %v", err)
	}
	if len(flags) != 1 || flags[0].Description != "updated rollout" {
		t.Fatalf("ListFlags() = %#v, want single updated flag", flags)
	}

	if err := svc.DeleteFlag(ctx, "new-ui"); err != nil {
		t.Fatalf("DeleteFlag() error = %v", err)
	}

	if _, err := svc.GetFlag(ctx, "new-ui"); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("GetFlag() error = %v, want %v", err, ErrFlagNotFound)
	}

	repo.mu.RLock()
	events := append([]repository.FlagEvent(nil), repo.events...)
	repo.mu.RUnlock()
	if len(events) != 3 {
		t.Fatalf("PublishFlagEvent calls = %d, want 3", len(events))
	}
	if events[0].EventType != EventTypeUpdated || events[1].EventType != EventTypeUpdated || events[2].EventType != EventTypeDeleted {
		t.Fatalf("event types = %#v, want [updated updated deleted]", []string{events[0].EventType, events[1].EventType, events[2].EventType})
	}
}

func TestServiceMutationSucceedsWhenPublishFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeServiceRepository()
	repo.publishErr = errors.New("publish failed")

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	flag := repository.Flag{
		Key:         "new-ui",
		Description: "initial rollout",
		Enabled:     true,
		Variants:    json.RawMessage(`{}`),
		Rules:       json.RawMessage(`[]`),
	}

	created, err := svc.CreateFlag(ctx, flag)
	if err != nil {
		t.Fatalf("CreateFlag() error = %v, want nil when publish fails", err)
	}
	if created.Key != flag.Key {
		t.Fatalf("CreateFlag().Key = %q, want %q", created.Key, flag.Key)
	}

	flag.Description = "updated rollout"
	if _, err := svc.UpdateFlag(ctx, flag); err != nil {
		t.Fatalf("UpdateFlag() error = %v, want nil when publish fails", err)
	}

	if err := svc.DeleteFlag(ctx, flag.Key); err != nil {
		t.Fatalf("DeleteFlag() error = %v, want nil when publish fails", err)
	}

	if _, err := svc.GetFlag(ctx, flag.Key); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("GetFlag() error = %v, want %v", err, ErrFlagNotFound)
	}
}

func TestServiceUpdateFlagEvictsStaleCacheOnNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeServiceRepository()
	flag := repository.Flag{
		Key:     "new-ui",
		Enabled: true,
	}
	repo.setFlag(flag)

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	repo.removeFlag(flag.Key)
	_, err = svc.UpdateFlag(ctx, repository.Flag{
		Key:         flag.Key,
		Description: "updated",
		Enabled:     true,
	})
	if !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("UpdateFlag() error = %v, want %v", err, ErrFlagNotFound)
	}

	if _, err := svc.GetFlag(ctx, flag.Key); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("GetFlag() error = %v, want %v", err, ErrFlagNotFound)
	}
}

func TestServiceDeleteFlagEvictsStaleCacheOnNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeServiceRepository()
	flag := repository.Flag{
		Key:     "new-ui",
		Enabled: true,
	}
	repo.setFlag(flag)

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	repo.removeFlag(flag.Key)
	if err := svc.DeleteFlag(ctx, flag.Key); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("DeleteFlag() error = %v, want %v", err, ErrFlagNotFound)
	}

	if _, err := svc.GetFlag(ctx, flag.Key); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("GetFlag() error = %v, want %v", err, ErrFlagNotFound)
	}
}

func TestServiceRejectsInvalidRules(t *testing.T) {
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		repo := newFakeServiceRepository()
		svc, err := New(ctx, repo)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, err = svc.CreateFlag(ctx, repository.Flag{
			Key:         "new-ui",
			Description: "initial rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{}`),
			Rules:       json.RawMessage(`{"attribute":"country"}`),
		})
		if !errors.Is(err, ErrInvalidRules) {
			t.Fatalf("CreateFlag() error = %v, want %v", err, ErrInvalidRules)
		}

		flags, err := svc.ListFlags(ctx)
		if err != nil {
			t.Fatalf("ListFlags() error = %v", err)
		}
		if len(flags) != 0 {
			t.Fatalf("ListFlags() len = %d, want 0", len(flags))
		}
	})

	t.Run("update", func(t *testing.T) {
		repo := newFakeServiceRepository()
		repo.setFlag(repository.Flag{
			Key:         "new-ui",
			Description: "initial rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{}`),
			Rules:       json.RawMessage(`[]`),
		})
		svc, err := New(ctx, repo)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, err = svc.UpdateFlag(ctx, repository.Flag{
			Key:         "new-ui",
			Description: "updated rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{}`),
			Rules:       json.RawMessage(`{"attribute":"country"}`),
		})
		if !errors.Is(err, ErrInvalidRules) {
			t.Fatalf("UpdateFlag() error = %v, want %v", err, ErrInvalidRules)
		}

		got, err := svc.GetFlag(ctx, "new-ui")
		if err != nil {
			t.Fatalf("GetFlag() error = %v", err)
		}
		if got.Description != "initial rollout" {
			t.Fatalf("GetFlag().Description = %q, want %q", got.Description, "initial rollout")
		}
	})
}

func TestServiceRejectsInvalidVariants(t *testing.T) {
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		repo := newFakeServiceRepository()
		svc, err := New(ctx, repo)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, err = svc.CreateFlag(ctx, repository.Flag{
			Key:         "new-ui",
			Description: "initial rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{"control":`),
			Rules:       json.RawMessage(`[]`),
		})
		if !errors.Is(err, ErrInvalidVariants) {
			t.Fatalf("CreateFlag() error = %v, want %v", err, ErrInvalidVariants)
		}

		flags, err := svc.ListFlags(ctx)
		if err != nil {
			t.Fatalf("ListFlags() error = %v", err)
		}
		if len(flags) != 0 {
			t.Fatalf("ListFlags() len = %d, want 0", len(flags))
		}
	})

	t.Run("update", func(t *testing.T) {
		repo := newFakeServiceRepository()
		repo.setFlag(repository.Flag{
			Key:         "new-ui",
			Description: "initial rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{}`),
			Rules:       json.RawMessage(`[]`),
		})
		svc, err := New(ctx, repo)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		_, err = svc.UpdateFlag(ctx, repository.Flag{
			Key:         "new-ui",
			Description: "updated rollout",
			Enabled:     true,
			Variants:    json.RawMessage(`{"control":`),
			Rules:       json.RawMessage(`[]`),
		})
		if !errors.Is(err, ErrInvalidVariants) {
			t.Fatalf("UpdateFlag() error = %v, want %v", err, ErrInvalidVariants)
		}

		got, err := svc.GetFlag(ctx, "new-ui")
		if err != nil {
			t.Fatalf("GetFlag() error = %v", err)
		}
		if got.Description != "initial rollout" {
			t.Fatalf("GetFlag().Description = %q, want %q", got.Description, "initial rollout")
		}
	})
}

func TestServiceMutationPublishesWithDetachedContext(t *testing.T) {
	repo := newFakeServiceRepository()
	repo.requirePublishActiveContext = true

	svc, err := New(context.Background(), repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flag := repository.Flag{
		Key:         "new-ui",
		Description: "initial rollout",
		Enabled:     true,
		Variants:    json.RawMessage(`{}`),
		Rules:       json.RawMessage(`[]`),
	}
	if _, err := svc.CreateFlag(ctx, flag); err != nil {
		t.Fatalf("CreateFlag() error = %v, want nil even when request context is canceled", err)
	}

	repo.mu.RLock()
	defer repo.mu.RUnlock()
	if len(repo.events) != 1 {
		t.Fatalf("PublishFlagEvent calls = %d, want 1", len(repo.events))
	}
	if repo.publishCtxErr != nil {
		t.Fatalf("publish context error = %v, want nil", repo.publishCtxErr)
	}
	if !repo.publishCtxHasDeadline {
		t.Fatal("publish context has no deadline, want timeout")
	}
}

func TestServiceRefreshesCacheFromInvalidations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := newNotifyingFakeServiceRepository()
	initial := repository.Flag{
		Key:         "new-ui",
		Description: "initial rollout",
		Enabled:     false,
		Variants:    json.RawMessage(`{}`),
		Rules:       json.RawMessage(`[]`),
	}
	repo.setFlag(initial)

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	updated := initial
	updated.Description = "updated remotely"
	updated.Enabled = true
	repo.setFlag(updated)

	stale, err := svc.GetFlag(ctx, initial.Key)
	if err != nil {
		t.Fatalf("GetFlag() error = %v", err)
	}
	if stale.Description != initial.Description {
		t.Fatalf("GetFlag().Description = %q, want stale %q before invalidation", stale.Description, initial.Description)
	}

	repo.notifyInvalidation()
	waitForCondition(t, time.Second, func() bool {
		flag, err := svc.GetFlag(ctx, initial.Key)
		return err == nil && flag.Description == updated.Description && flag.Enabled == updated.Enabled
	})

	repo.removeFlag(initial.Key)
	repo.notifyInvalidation()
	waitForCondition(t, time.Second, func() bool {
		_, err := svc.GetFlag(ctx, initial.Key)
		return errors.Is(err, ErrFlagNotFound)
	})
}

func TestServiceResolveBooleanUsesVariantsDefaultFallback(t *testing.T) {
	ctx := context.Background()
	repo := newFakeServiceRepository()
	repo.setFlag(repository.Flag{
		Key:         "new-ui",
		Description: "rollout",
		Enabled:     true,
		Variants:    json.RawMessage(`{"default":false}`),
		Rules:       json.RawMessage(`[{"attribute":"country","operator":"equals","value":"US"}]`),
	})

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mismatch, err := svc.ResolveBoolean(ctx, "new-ui", core.EvaluationContext{
		Attributes: map[string]any{"country": "CA"},
	}, true)
	if err != nil {
		t.Fatalf("ResolveBoolean() error = %v", err)
	}
	if mismatch {
		t.Fatalf("ResolveBoolean() = %t, want false from variants.default fallback", mismatch)
	}

	match, err := svc.ResolveBoolean(ctx, "new-ui", core.EvaluationContext{
		Attributes: map[string]any{"country": "US"},
	}, false)
	if err != nil {
		t.Fatalf("ResolveBoolean() error = %v", err)
	}
	if !match {
		t.Fatalf("ResolveBoolean() = %t, want true when rule matches", match)
	}
}

func TestServiceResubscribesAfterInvalidationChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := newResubscribingFakeServiceRepository()
	initial := repository.Flag{
		Key:         "new-ui",
		Description: "initial rollout",
		Enabled:     false,
		Variants:    json.RawMessage(`{}`),
		Rules:       json.RawMessage(`[]`),
	}
	repo.setFlag(initial)

	svc, err := New(ctx, repo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	updated := initial
	updated.Description = "updated remotely"
	updated.Enabled = true
	repo.setFlag(updated)

	stale, err := svc.GetFlag(ctx, initial.Key)
	if err != nil {
		t.Fatalf("GetFlag() error = %v", err)
	}
	if stale.Description != initial.Description {
		t.Fatalf("GetFlag().Description = %q, want stale %q before invalidation", stale.Description, initial.Description)
	}

	repo.closeInvalidationChannel()
	waitForCondition(t, time.Second, func() bool {
		return repo.subscriptionCalls() >= 2
	})

	repo.notifyInvalidation()
	waitForCondition(t, time.Second, func() bool {
		flag, err := svc.GetFlag(ctx, initial.Key)
		return err == nil && flag.Description == updated.Description && flag.Enabled == updated.Enabled
	})
}

type fakeServiceRepository struct {
	mu          sync.RWMutex
	flags       map[string]repository.Flag
	events      []repository.FlagEvent
	nextEventID int64
	publishErr  error

	requirePublishActiveContext bool
	publishCtxErr               error
	publishCtxHasDeadline       bool
}

func newFakeServiceRepository() *fakeServiceRepository {
	return &fakeServiceRepository{
		flags: make(map[string]repository.Flag),
	}
}

func (f *fakeServiceRepository) CreateFlag(_ context.Context, flag repository.Flag) (repository.Flag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.flags[flag.Key] = flag
	return flag, nil
}

func (f *fakeServiceRepository) UpdateFlag(_ context.Context, flag repository.Flag) (repository.Flag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.flags[flag.Key]; !ok {
		return repository.Flag{}, pgx.ErrNoRows
	}
	f.flags[flag.Key] = flag
	return flag, nil
}

func (f *fakeServiceRepository) GetFlag(_ context.Context, key string) (repository.Flag, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	flag, ok := f.flags[key]
	if !ok {
		return repository.Flag{}, pgx.ErrNoRows
	}
	return flag, nil
}

func (f *fakeServiceRepository) ListFlags(_ context.Context) ([]repository.Flag, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	flags := make([]repository.Flag, 0, len(f.flags))
	for _, flag := range f.flags {
		flags = append(flags, flag)
	}
	return flags, nil
}

func (f *fakeServiceRepository) DeleteFlag(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.flags[key]; !ok {
		return pgx.ErrNoRows
	}
	delete(f.flags, key)
	return nil
}

func (f *fakeServiceRepository) ListEventsSince(_ context.Context, eventID int64) ([]repository.FlagEvent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	events := make([]repository.FlagEvent, 0, len(f.events))
	for _, event := range f.events {
		if event.EventID > eventID {
			events = append(events, event)
		}
	}
	return events, nil
}

func (f *fakeServiceRepository) ListEventsSinceForKey(_ context.Context, eventID int64, key string) ([]repository.FlagEvent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	events := make([]repository.FlagEvent, 0, len(f.events))
	for _, event := range f.events {
		if event.EventID > eventID && event.FlagKey == key {
			events = append(events, event)
		}
	}
	return events, nil
}

func (f *fakeServiceRepository) PublishFlagEvent(ctx context.Context, event repository.FlagEvent) (repository.FlagEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.publishCtxErr = ctx.Err()
	_, f.publishCtxHasDeadline = ctx.Deadline()

	if f.requirePublishActiveContext && f.publishCtxErr != nil {
		return repository.FlagEvent{}, f.publishCtxErr
	}

	if f.publishErr != nil {
		return repository.FlagEvent{}, f.publishErr
	}

	f.nextEventID++
	event.EventID = f.nextEventID
	f.events = append(f.events, event)
	return event, nil
}

func (f *fakeServiceRepository) setFlag(flag repository.Flag) {
	f.mu.Lock()
	f.flags[flag.Key] = flag
	f.mu.Unlock()
}

func (f *fakeServiceRepository) removeFlag(key string) {
	f.mu.Lock()
	delete(f.flags, key)
	f.mu.Unlock()
}

type notifyingFakeServiceRepository struct {
	*fakeServiceRepository
	invalidations chan struct{}
}

func newNotifyingFakeServiceRepository() *notifyingFakeServiceRepository {
	return &notifyingFakeServiceRepository{
		fakeServiceRepository: newFakeServiceRepository(),
		invalidations:         make(chan struct{}, 1),
	}
}

func (f *notifyingFakeServiceRepository) SubscribeFlagInvalidation(_ context.Context) (<-chan struct{}, error) {
	return f.invalidations, nil
}

func (f *notifyingFakeServiceRepository) notifyInvalidation() {
	select {
	case f.invalidations <- struct{}{}:
	default:
	}
}

type resubscribingFakeServiceRepository struct {
	*fakeServiceRepository
	invalidationMu sync.Mutex
	invalidations  chan struct{}
	subscriptions  int
}

func newResubscribingFakeServiceRepository() *resubscribingFakeServiceRepository {
	return &resubscribingFakeServiceRepository{
		fakeServiceRepository: newFakeServiceRepository(),
		invalidations:         make(chan struct{}, 1),
	}
}

func (f *resubscribingFakeServiceRepository) SubscribeFlagInvalidation(_ context.Context) (<-chan struct{}, error) {
	f.invalidationMu.Lock()
	defer f.invalidationMu.Unlock()

	if f.invalidations == nil {
		f.invalidations = make(chan struct{}, 1)
	}
	f.subscriptions++
	return f.invalidations, nil
}

func (f *resubscribingFakeServiceRepository) closeInvalidationChannel() {
	f.invalidationMu.Lock()
	ch := f.invalidations
	f.invalidations = nil
	f.invalidationMu.Unlock()

	if ch != nil {
		close(ch)
	}
}

func (f *resubscribingFakeServiceRepository) notifyInvalidation() {
	f.invalidationMu.Lock()
	ch := f.invalidations
	f.invalidationMu.Unlock()
	if ch == nil {
		return
	}

	select {
	case ch <- struct{}{}:
	default:
	}
}

func (f *resubscribingFakeServiceRepository) subscriptionCalls() int {
	f.invalidationMu.Lock()
	defer f.invalidationMu.Unlock()
	return f.subscriptions
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition not met before timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
