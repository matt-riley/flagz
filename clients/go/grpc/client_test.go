package grpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	flagz "github.com/matt-riley/flagz/clients/go"
	flagzgrpc "github.com/matt-riley/flagz/clients/go/grpc"
	flagspb "github.com/matt-riley/flagz/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20 // 1 MiB

// testServer is a minimal in-process FlagService gRPC server.
type testServer struct {
	flagspb.UnimplementedFlagServiceServer
	flags    map[string]*flagspb.Flag
	capturedMD metadata.MD
}

func newTestServer() *testServer {
	return &testServer{flags: map[string]*flagspb.Flag{}}
}

func (s *testServer) captureAuth(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		s.capturedMD = md
	}
}

func (s *testServer) assertAuth(t *testing.T) {
	t.Helper()
	vals := s.capturedMD.Get("authorization")
	if len(vals) == 0 || vals[0] != "Bearer test-key" {
		t.Errorf("auth metadata: got %v, want [Bearer test-key]", vals)
	}
}

func (s *testServer) CreateFlag(ctx context.Context, req *flagspb.CreateFlagRequest) (*flagspb.CreateFlagResponse, error) {
	s.captureAuth(ctx)
	s.flags[req.Flag.Key] = req.Flag
	return &flagspb.CreateFlagResponse{Flag: req.Flag}, nil
}

func (s *testServer) GetFlag(ctx context.Context, req *flagspb.GetFlagRequest) (*flagspb.GetFlagResponse, error) {
	s.captureAuth(ctx)
	f, ok := s.flags[req.Key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", req.Key)
	}
	return &flagspb.GetFlagResponse{Flag: f}, nil
}

func (s *testServer) ListFlags(ctx context.Context, _ *flagspb.ListFlagsRequest) (*flagspb.ListFlagsResponse, error) {
	s.captureAuth(ctx)
	flags := make([]*flagspb.Flag, 0, len(s.flags))
	for _, f := range s.flags {
		flags = append(flags, f)
	}
	return &flagspb.ListFlagsResponse{Flags: flags}, nil
}

func (s *testServer) UpdateFlag(ctx context.Context, req *flagspb.UpdateFlagRequest) (*flagspb.UpdateFlagResponse, error) {
	s.captureAuth(ctx)
	s.flags[req.Flag.Key] = req.Flag
	return &flagspb.UpdateFlagResponse{Flag: req.Flag}, nil
}

func (s *testServer) DeleteFlag(ctx context.Context, req *flagspb.DeleteFlagRequest) (*flagspb.DeleteFlagResponse, error) {
	s.captureAuth(ctx)
	delete(s.flags, req.Key)
	return &flagspb.DeleteFlagResponse{}, nil
}

func (s *testServer) ResolveBoolean(ctx context.Context, req *flagspb.ResolveBooleanRequest) (*flagspb.ResolveBooleanResponse, error) {
	s.captureAuth(ctx)
	f, ok := s.flags[req.Key]
	if !ok {
		return &flagspb.ResolveBooleanResponse{Key: req.Key, Value: req.DefaultValue}, nil
	}
	return &flagspb.ResolveBooleanResponse{Key: req.Key, Value: f.Enabled}, nil
}

func (s *testServer) ResolveBatch(ctx context.Context, req *flagspb.ResolveBatchRequest) (*flagspb.ResolveBatchResponse, error) {
	s.captureAuth(ctx)
	results := make([]*flagspb.ResolveBatchResult, len(req.Requests))
	for i, r := range req.Requests {
		f, ok := s.flags[r.Key]
		val := r.DefaultValue
		if ok {
			val = f.Enabled
		}
		results[i] = &flagspb.ResolveBatchResult{Key: r.Key, Value: val}
	}
	return &flagspb.ResolveBatchResponse{Results: results}, nil
}

func (s *testServer) WatchFlag(req *flagspb.WatchFlagRequest, stream flagspb.FlagService_WatchFlagServer) error {
	s.captureAuth(stream.Context())
	// Emit two events then return.
	events := []*flagspb.WatchFlagEvent{
		{Type: flagspb.WatchFlagEventType_FLAG_UPDATED, Key: "flag-a", EventId: 1, Flag: &flagspb.Flag{Key: "flag-a", Enabled: true}},
		{Type: flagspb.WatchFlagEventType_FLAG_DELETED, Key: "flag-b", EventId: 2},
	}
	for _, ev := range events {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}
	return nil
}

// -- test harness ------------------------------------------------------------

func startTestServer(t *testing.T) (*testServer, *flagzgrpc.Client) {
	t.Helper()
	ts := newTestServer()
	lis := bufconn.Listen(bufSize)
	gs := grpc.NewServer()
	flagspb.RegisterFlagServiceServer(gs, ts)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.Stop(); lis.Close() })

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	}
	c, err := flagzgrpc.NewGRPCClient(flagzgrpc.Config{
		Address:  "passthrough:///bufnet",
		APIKey:   "test-key",
		DialOpts: dialOpts,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return ts, c
}

// -- CRUD tests --------------------------------------------------------------

func TestGRPCCreateFlag(t *testing.T) {
	ts, c := startTestServer(t)

	variantsJSON, _ := json.Marshal(map[string]bool{"beta": true})
	f, err := c.CreateFlag(context.Background(), flagz.Flag{
		Key:      "my-flag",
		Enabled:  true,
		Variants: map[string]bool{"beta": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Key != "my-flag" || !f.Enabled {
		t.Errorf("unexpected flag: %+v", f)
	}
	if f.Variants["beta"] != true {
		t.Errorf("variants not round-tripped: %+v", f.Variants)
	}
	ts.assertAuth(t)
	_ = variantsJSON
}

func TestGRPCGetFlag(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["x"] = &flagspb.Flag{Key: "x", Enabled: true}

	f, err := c.GetFlag(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if f.Key != "x" || !f.Enabled {
		t.Errorf("unexpected flag: %+v", f)
	}
	ts.assertAuth(t)
}

func TestGRPCListFlags(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["a"] = &flagspb.Flag{Key: "a", Enabled: true}
	ts.flags["b"] = &flagspb.Flag{Key: "b", Enabled: false}

	flags, err := c.ListFlags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("want 2 flags, got %d", len(flags))
	}
}

func TestGRPCUpdateFlag(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["x"] = &flagspb.Flag{Key: "x", Enabled: true}

	f, err := c.UpdateFlag(context.Background(), flagz.Flag{Key: "x", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if f.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestGRPCDeleteFlag(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["x"] = &flagspb.Flag{Key: "x", Enabled: true}

	if err := c.DeleteFlag(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ts.flags["x"]; ok {
		t.Error("flag should be deleted")
	}
}

// -- Evaluator tests ---------------------------------------------------------

func TestGRPCEvaluate(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["flag-on"] = &flagspb.Flag{Key: "flag-on", Enabled: true}

	v, err := c.Evaluate(context.Background(), "flag-on", flagz.EvaluationContext{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Error("expected true")
	}
	ts.assertAuth(t)
}

func TestGRPCEvaluateMissing(t *testing.T) {
	_, c := startTestServer(t)
	v, err := c.Evaluate(context.Background(), "missing", flagz.EvaluationContext{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Error("expected default=true")
	}
}

func TestGRPCEvaluateBatch(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["a"] = &flagspb.Flag{Key: "a", Enabled: true}
	ts.flags["b"] = &flagspb.Flag{Key: "b", Enabled: false}

	results, err := c.EvaluateBatch(context.Background(), []flagz.EvaluateRequest{
		{Key: "a", DefaultValue: false},
		{Key: "b", DefaultValue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2, got %d", len(results))
	}
	if !results[0].Value || results[1].Value {
		t.Errorf("unexpected values: %+v", results)
	}
}

// -- Streamer tests ----------------------------------------------------------

func TestGRPCStream(t *testing.T) {
	ts, c := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := c.Stream(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var received []flagz.FlagEvent
	for ev := range ch {
		received = append(received, ev)
	}

	if len(received) != 2 {
		t.Fatalf("want 2 events, got %d", len(received))
	}
	if received[0].Type != "update" || received[0].EventID != 1 {
		t.Errorf("event 0: %+v", received[0])
	}
	if received[1].Type != "delete" || received[1].EventID != 2 {
		t.Errorf("event 1: %+v", received[1])
	}
	ts.assertAuth(t)
}

func TestGRPCStreamContextCancel(t *testing.T) {
	// Override WatchFlag to hold open until context cancels.
	lis := bufconn.Listen(bufSize)
	ts := &testServer{flags: map[string]*flagspb.Flag{}}
	// Use a custom server that blocks on WatchFlag.
	blocker := &blockingWatchServer{testServer: ts, lis: lis}
	gs := grpc.NewServer()
	flagspb.RegisterFlagServiceServer(gs, blocker)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.Stop(); lis.Close() })

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	}
	c, err := flagzgrpc.NewGRPCClient(flagzgrpc.Config{Address: "passthrough:///bufnet", APIKey: "k", DialOpts: dialOpts})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Stream(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.AfterFunc(100*time.Millisecond, cancel)

	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for stream to close")
		}
	}
}

type blockingWatchServer struct {
	*testServer
	lis *bufconn.Listener
}

func (b *blockingWatchServer) WatchFlag(_ *flagspb.WatchFlagRequest, stream flagspb.FlagService_WatchFlagServer) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

// -- wire mapping round-trip -------------------------------------------------

func TestGRPCVariantsRoundTrip(t *testing.T) {
	ts, c := startTestServer(t)
	ts.flags["x"] = &flagspb.Flag{Key: "x", Enabled: true}

	orig := flagz.Flag{
		Key:      "x",
		Enabled:  true,
		Variants: map[string]bool{"beta": true, "alpha": false},
		Rules: []flagz.Rule{
			{Attribute: "env", Operator: "equals", Value: "prod"},
		},
	}
	created, err := c.CreateFlag(context.Background(), orig)
	if err != nil {
		t.Fatal(err)
	}
	if created.Variants["beta"] != true || created.Variants["alpha"] != false {
		t.Errorf("variants: %+v", created.Variants)
	}
	if len(created.Rules) != 1 || created.Rules[0].Attribute != "env" {
		t.Errorf("rules: %+v", created.Rules)
	}
}

// -- compile-time interface checks -------------------------------------------

var _ flagz.FlagManager = (*flagzgrpc.Client)(nil)
var _ flagz.Evaluator = (*flagzgrpc.Client)(nil)
var _ flagz.Streamer = (*flagzgrpc.Client)(nil)

// Ensure testServer satisfies the interface at compile time.
var _ flagspb.FlagServiceServer = (*testServer)(nil)
var _ flagspb.FlagServiceServer = (*blockingWatchServer)(nil)
