package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	flagspb "github.com/mattriley/flagz/api/proto/v1"
	"github.com/mattriley/flagz/internal/repository"
	"github.com/mattriley/flagz/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCServerCreateFlag(t *testing.T) {
	t.Run("missing flag", func(t *testing.T) {
		grpcServer := NewGRPCServer(&fakeService{})

		_, err := grpcServer.CreateFlag(context.Background(), nil)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		svc := &fakeService{
			createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
				t.Fatal("CreateFlag should not be called")
				return repository.Flag{}, nil
			},
		}
		grpcServer := NewGRPCServer(svc)

		_, err := grpcServer.CreateFlag(context.Background(), &flagspb.CreateFlagRequest{
			Flag: &flagspb.Flag{},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("maps validation errors to invalid argument", func(t *testing.T) {
		svc := &fakeService{
			createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
				return repository.Flag{}, errors.New("flag key is required")
			},
		}
		grpcServer := NewGRPCServer(svc)

		_, err := grpcServer.CreateFlag(context.Background(), &flagspb.CreateFlagRequest{
			Flag: &flagspb.Flag{Key: "new-ui"},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("maps invalid rules errors to invalid argument", func(t *testing.T) {
		svc := &fakeService{
			createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
				return repository.Flag{}, service.ErrInvalidRules
			},
		}
		grpcServer := NewGRPCServer(svc)

		_, err := grpcServer.CreateFlag(context.Background(), &flagspb.CreateFlagRequest{
			Flag: &flagspb.Flag{Key: "new-ui"},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("maps invalid variants errors to invalid argument", func(t *testing.T) {
		svc := &fakeService{
			createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
				return repository.Flag{}, service.ErrInvalidVariants
			},
		}
		grpcServer := NewGRPCServer(svc)

		_, err := grpcServer.CreateFlag(context.Background(), &flagspb.CreateFlagRequest{
			Flag: &flagspb.Flag{Key: "new-ui"},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("creates flag", func(t *testing.T) {
		svc := &fakeService{
			createFlagFunc: func(_ context.Context, flag repository.Flag) (repository.Flag, error) {
				if flag.Key != "new-ui" {
					t.Fatalf("CreateFlag key = %q, want %q", flag.Key, "new-ui")
				}
				if !flag.Enabled {
					t.Fatal("CreateFlag enabled = false, want true")
				}
				return flag, nil
			},
		}
		grpcServer := NewGRPCServer(svc)

		resp, err := grpcServer.CreateFlag(context.Background(), &flagspb.CreateFlagRequest{
			Flag: &flagspb.Flag{
				Key:          "new-ui",
				Description:  "new ui rollout",
				Enabled:      true,
				VariantsJson: []byte(`{"control":true}`),
				RulesJson:    []byte(`[]`),
			},
		})
		if err != nil {
			t.Fatalf("CreateFlag() error = %v", err)
		}
		if resp.GetFlag().GetKey() != "new-ui" {
			t.Fatalf("CreateFlag().Flag.Key = %q, want %q", resp.GetFlag().GetKey(), "new-ui")
		}
	})
}

func TestGRPCServerUpdateFlag(t *testing.T) {
	t.Run("missing flag", func(t *testing.T) {
		grpcServer := NewGRPCServer(&fakeService{})

		_, err := grpcServer.UpdateFlag(context.Background(), nil)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("UpdateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		svc := &fakeService{
			updateFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
				t.Fatal("UpdateFlag should not be called")
				return repository.Flag{}, nil
			},
		}
		grpcServer := NewGRPCServer(svc)

		_, err := grpcServer.UpdateFlag(context.Background(), &flagspb.UpdateFlagRequest{
			Flag: &flagspb.Flag{},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("UpdateFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("updates flag", func(t *testing.T) {
		svc := &fakeService{
			updateFlagFunc: func(_ context.Context, flag repository.Flag) (repository.Flag, error) {
				if flag.Key != "new-ui" {
					t.Fatalf("UpdateFlag key = %q, want %q", flag.Key, "new-ui")
				}
				return flag, nil
			},
		}
		grpcServer := NewGRPCServer(svc)

		resp, err := grpcServer.UpdateFlag(context.Background(), &flagspb.UpdateFlagRequest{
			Flag: &flagspb.Flag{
				Key:         "new-ui",
				Description: "updated",
				Enabled:     true,
			},
		})
		if err != nil {
			t.Fatalf("UpdateFlag() error = %v", err)
		}
		if resp.GetFlag().GetKey() != "new-ui" {
			t.Fatalf("UpdateFlag().Flag.Key = %q, want %q", resp.GetFlag().GetKey(), "new-ui")
		}
	})
}

func TestGRPCServerListFlagsPagination(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "a"},
				{Key: "b"},
				{Key: "c"},
			}, nil
		},
	}
	grpcServer := NewGRPCServer(svc)

	t.Run("returns all flags when page size is not set", func(t *testing.T) {
		resp, err := grpcServer.ListFlags(context.Background(), &flagspb.ListFlagsRequest{})
		if err != nil {
			t.Fatalf("ListFlags() error = %v", err)
		}
		if len(resp.GetFlags()) != 3 {
			t.Fatalf("ListFlags() flags len = %d, want %d", len(resp.GetFlags()), 3)
		}
		if resp.GetNextPageToken() != "" {
			t.Fatalf("ListFlags() next_page_token = %q, want empty", resp.GetNextPageToken())
		}
	})

	t.Run("returns paginated results with next page token", func(t *testing.T) {
		resp, err := grpcServer.ListFlags(context.Background(), &flagspb.ListFlagsRequest{
			PageSize: 2,
		})
		if err != nil {
			t.Fatalf("ListFlags() error = %v", err)
		}
		if len(resp.GetFlags()) != 2 {
			t.Fatalf("ListFlags() flags len = %d, want %d", len(resp.GetFlags()), 2)
		}
		if resp.GetFlags()[0].GetKey() != "a" || resp.GetFlags()[1].GetKey() != "b" {
			t.Fatalf("ListFlags() keys = [%q %q], want [a b]", resp.GetFlags()[0].GetKey(), resp.GetFlags()[1].GetKey())
		}
		if resp.GetNextPageToken() != "2" {
			t.Fatalf("ListFlags() next_page_token = %q, want %q", resp.GetNextPageToken(), "2")
		}
	})

	t.Run("uses page token to return next page", func(t *testing.T) {
		resp, err := grpcServer.ListFlags(context.Background(), &flagspb.ListFlagsRequest{
			PageSize:  2,
			PageToken: "2",
		})
		if err != nil {
			t.Fatalf("ListFlags() error = %v", err)
		}
		if len(resp.GetFlags()) != 1 {
			t.Fatalf("ListFlags() flags len = %d, want %d", len(resp.GetFlags()), 1)
		}
		if resp.GetFlags()[0].GetKey() != "c" {
			t.Fatalf("ListFlags() key = %q, want %q", resp.GetFlags()[0].GetKey(), "c")
		}
		if resp.GetNextPageToken() != "" {
			t.Fatalf("ListFlags() next_page_token = %q, want empty", resp.GetNextPageToken())
		}
	})

	t.Run("rejects invalid page token", func(t *testing.T) {
		_, err := grpcServer.ListFlags(context.Background(), &flagspb.ListFlagsRequest{
			PageSize:  1,
			PageToken: "bad",
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListFlags() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})

	t.Run("rejects negative page size", func(t *testing.T) {
		_, err := grpcServer.ListFlags(context.Background(), &flagspb.ListFlagsRequest{
			PageSize: -1,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ListFlags() code = %v, want %v", status.Code(err), codes.InvalidArgument)
		}
	})
}

func TestGRPCServerWatchFlagStartsFromLastEventID(t *testing.T) {
	sinceCalls := make([]int64, 0)
	keyCalls := make([]string, 0)
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeWatchFlagServer{
		ctx:    ctx,
		cancel: cancel,
	}

	svc := &fakeService{
		listEventsSinceForKeyFunc: func(_ context.Context, eventID int64, key string) ([]repository.FlagEvent, error) {
			sinceCalls = append(sinceCalls, eventID)
			keyCalls = append(keyCalls, key)
			if eventID != 5 {
				return nil, nil
			}
			return []repository.FlagEvent{
				{
					EventID:   6,
					FlagKey:   "new-ui",
					EventType: "updated",
					Payload:   json.RawMessage(`{"key":"new-ui","enabled":true}`),
				},
			}, nil
		},
	}
	grpcServer := NewGRPCServerWithStreamPollInterval(svc, time.Hour)

	err := grpcServer.WatchFlag(&flagspb.WatchFlagRequest{
		Key:         "new-ui",
		LastEventId: 5,
	}, stream)
	if err != nil {
		t.Fatalf("WatchFlag() error = %v", err)
	}
	if len(sinceCalls) == 0 || sinceCalls[0] != 5 {
		t.Fatalf("first ListEventsSinceForKey call = %#v, want first value %d", sinceCalls, 5)
	}
	if len(keyCalls) == 0 || keyCalls[0] != "new-ui" {
		t.Fatalf("first ListEventsSinceForKey key = %#v, want first value %q", keyCalls, "new-ui")
	}
	if len(stream.events) != 1 {
		t.Fatalf("WatchFlag() sent %d events, want 1", len(stream.events))
	}
	if stream.events[0].GetKey() != "new-ui" {
		t.Fatalf("WatchFlag() event key = %q, want %q", stream.events[0].GetKey(), "new-ui")
	}
	if stream.events[0].GetEventId() != 6 {
		t.Fatalf("WatchFlag() event id = %d, want %d", stream.events[0].GetEventId(), 6)
	}
}

func TestGRPCServerWatchFlagRejectsNegativeLastEventID(t *testing.T) {
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ int64) ([]repository.FlagEvent, error) {
			t.Fatal("ListEventsSince should not be called")
			return nil, nil
		},
		listEventsSinceForKeyFunc: func(_ context.Context, _ int64, _ string) ([]repository.FlagEvent, error) {
			t.Fatal("ListEventsSinceForKey should not be called")
			return nil, nil
		},
	}
	grpcServer := NewGRPCServerWithStreamPollInterval(svc, time.Hour)

	err := grpcServer.WatchFlag(&flagspb.WatchFlagRequest{LastEventId: -1}, &fakeWatchFlagServer{
		ctx: context.Background(),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("WatchFlag() code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func TestGRPCServerWatchFlagWithoutKeyUsesUnfilteredEvents(t *testing.T) {
	sinceCalls := make([]int64, 0)
	keyedCalls := 0
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeWatchFlagServer{
		ctx:    ctx,
		cancel: cancel,
	}

	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, eventID int64) ([]repository.FlagEvent, error) {
			sinceCalls = append(sinceCalls, eventID)
			if eventID != 3 {
				return nil, nil
			}
			return []repository.FlagEvent{
				{
					EventID:   4,
					FlagKey:   "new-ui",
					EventType: "updated",
					Payload:   json.RawMessage(`{"key":"new-ui","enabled":true}`),
				},
			}, nil
		},
		listEventsSinceForKeyFunc: func(_ context.Context, _ int64, _ string) ([]repository.FlagEvent, error) {
			keyedCalls++
			return nil, nil
		},
	}
	grpcServer := NewGRPCServerWithStreamPollInterval(svc, time.Hour)

	err := grpcServer.WatchFlag(&flagspb.WatchFlagRequest{
		LastEventId: 3,
	}, stream)
	if err != nil {
		t.Fatalf("WatchFlag() error = %v", err)
	}
	if len(sinceCalls) == 0 || sinceCalls[0] != 3 {
		t.Fatalf("first ListEventsSince call = %#v, want first value %d", sinceCalls, 3)
	}
	if keyedCalls != 0 {
		t.Fatalf("ListEventsSinceForKey calls = %d, want %d", keyedCalls, 0)
	}
}

type fakeWatchFlagServer struct {
	ctx    context.Context
	cancel context.CancelFunc
	events []*flagspb.WatchFlagEvent
}

func (f *fakeWatchFlagServer) Send(event *flagspb.WatchFlagEvent) error {
	f.events = append(f.events, event)
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	return nil
}

func (f *fakeWatchFlagServer) SetHeader(metadata.MD) error {
	return nil
}

func (f *fakeWatchFlagServer) SendHeader(metadata.MD) error {
	return nil
}

func (f *fakeWatchFlagServer) SetTrailer(metadata.MD) {}

func (f *fakeWatchFlagServer) Context() context.Context {
	return f.ctx
}

func (f *fakeWatchFlagServer) SendMsg(any) error {
	return nil
}

func (f *fakeWatchFlagServer) RecvMsg(any) error {
	return io.EOF
}
