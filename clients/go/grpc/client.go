// Package grpc provides a gRPC client for the flagz feature flag service.
package grpc

import (
	"context"
	"encoding/json"
	"fmt"

	flagz "github.com/matt-riley/flagz/clients/go"
	flagspb "github.com/matt-riley/flagz/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Config holds configuration for the gRPC client.
type Config struct {
	// Address is the host:port of the flagz gRPC server, e.g. "localhost:9090".
	Address string
	// APIKey is the bearer token in "id.secret" format.
	APIKey string
	// DialOpts are additional gRPC dial options (e.g. TLS credentials).
	// If empty, insecure credentials are used.
	DialOpts []grpc.DialOption
}

// Client implements flagz.FlagManager, flagz.Evaluator, and flagz.Streamer over gRPC.
type Client struct {
	cfg    Config
	stub   flagspb.FlagServiceClient
	conn   *grpc.ClientConn
}

// NewGRPCClient dials the flagz gRPC server and returns a new client.
// Call Close() when done.
func NewGRPCClient(cfg Config) (*Client, error) {
	opts := []grpc.DialOption{}
	if len(cfg.DialOpts) > 0 {
		opts = append(opts, cfg.DialOpts...)
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("flagz: grpc dial: %w", err)
	}
	return &Client{cfg: cfg, stub: flagspb.NewFlagServiceClient(conn), conn: conn}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// authCtx injects the bearer token into outgoing gRPC metadata.
func (c *Client) authCtx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.cfg.APIKey)
}

// -- wire helpers ------------------------------------------------------------

func protoToFlag(p *flagspb.Flag) (flagz.Flag, error) {
	f := flagz.Flag{
		Key:         p.Key,
		Description: p.Description,
		Enabled:     p.Enabled,
	}
	if len(p.VariantsJson) > 0 {
		if err := json.Unmarshal(p.VariantsJson, &f.Variants); err != nil {
			return f, fmt.Errorf("flagz: decode variants_json: %w", err)
		}
	}
	if len(p.RulesJson) > 0 {
		var rules []struct {
			Attribute string `json:"attribute"`
			Operator  string `json:"operator"`
			Value     any    `json:"value"`
		}
		if err := json.Unmarshal(p.RulesJson, &rules); err != nil {
			return f, fmt.Errorf("flagz: decode rules_json: %w", err)
		}
		f.Rules = make([]flagz.Rule, len(rules))
		for i, r := range rules {
			f.Rules[i] = flagz.Rule{Attribute: r.Attribute, Operator: r.Operator, Value: r.Value}
		}
	}
	return f, nil
}

func flagToProto(f flagz.Flag) (*flagspb.Flag, error) {
	p := &flagspb.Flag{
		Key:         f.Key,
		Description: f.Description,
		Enabled:     f.Enabled,
	}
	if len(f.Variants) > 0 {
		b, err := json.Marshal(f.Variants)
		if err != nil {
			return nil, fmt.Errorf("flagz: encode variants: %w", err)
		}
		p.VariantsJson = b
	}
	if len(f.Rules) > 0 {
		rules := make([]struct {
			Attribute string `json:"attribute"`
			Operator  string `json:"operator"`
			Value     any    `json:"value"`
		}, len(f.Rules))
		for i, r := range f.Rules {
			rules[i].Attribute = r.Attribute
			rules[i].Operator = r.Operator
			rules[i].Value = r.Value
		}
		b, err := json.Marshal(rules)
		if err != nil {
			return nil, fmt.Errorf("flagz: encode rules: %w", err)
		}
		p.RulesJson = b
	}
	return p, nil
}

// -- FlagManager -------------------------------------------------------------

func (c *Client) CreateFlag(ctx context.Context, flag flagz.Flag) (flagz.Flag, error) {
	p, err := flagToProto(flag)
	if err != nil {
		return flagz.Flag{}, err
	}
	resp, err := c.stub.CreateFlag(c.authCtx(ctx), &flagspb.CreateFlagRequest{Flag: p})
	if err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: CreateFlag: %w", err)
	}
	return protoToFlag(resp.Flag)
}

func (c *Client) GetFlag(ctx context.Context, key string) (flagz.Flag, error) {
	resp, err := c.stub.GetFlag(c.authCtx(ctx), &flagspb.GetFlagRequest{Key: key})
	if err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: GetFlag: %w", err)
	}
	return protoToFlag(resp.Flag)
}

func (c *Client) ListFlags(ctx context.Context) ([]flagz.Flag, error) {
	resp, err := c.stub.ListFlags(c.authCtx(ctx), &flagspb.ListFlagsRequest{})
	if err != nil {
		return nil, fmt.Errorf("flagz: ListFlags: %w", err)
	}
	flags := make([]flagz.Flag, 0, len(resp.Flags))
	for _, p := range resp.Flags {
		f, err := protoToFlag(p)
		if err != nil {
			return nil, err
		}
		flags = append(flags, f)
	}
	return flags, nil
}

func (c *Client) UpdateFlag(ctx context.Context, flag flagz.Flag) (flagz.Flag, error) {
	p, err := flagToProto(flag)
	if err != nil {
		return flagz.Flag{}, err
	}
	resp, err := c.stub.UpdateFlag(c.authCtx(ctx), &flagspb.UpdateFlagRequest{Flag: p})
	if err != nil {
		return flagz.Flag{}, fmt.Errorf("flagz: UpdateFlag: %w", err)
	}
	return protoToFlag(resp.Flag)
}

func (c *Client) DeleteFlag(ctx context.Context, key string) error {
	_, err := c.stub.DeleteFlag(c.authCtx(ctx), &flagspb.DeleteFlagRequest{Key: key})
	if err != nil {
		return fmt.Errorf("flagz: DeleteFlag: %w", err)
	}
	return nil
}

// -- Evaluator ---------------------------------------------------------------

func (c *Client) Evaluate(ctx context.Context, key string, evalCtx flagz.EvaluationContext, defaultValue bool) (bool, error) {
	ctxJSON, err := json.Marshal(evalCtx)
	if err != nil {
		return defaultValue, fmt.Errorf("flagz: marshal context: %w", err)
	}
	resp, err := c.stub.ResolveBoolean(c.authCtx(ctx), &flagspb.ResolveBooleanRequest{
		Key:          key,
		ContextJson:  ctxJSON,
		DefaultValue: defaultValue,
	})
	if err != nil {
		return defaultValue, fmt.Errorf("flagz: ResolveBoolean: %w", err)
	}
	return resp.Value, nil
}

func (c *Client) EvaluateBatch(ctx context.Context, reqs []flagz.EvaluateRequest) ([]flagz.EvaluateResult, error) {
	pbReqs := make([]*flagspb.ResolveBooleanRequest, len(reqs))
	for i, r := range reqs {
		ctxJSON, err := json.Marshal(r.Context)
		if err != nil {
			return nil, fmt.Errorf("flagz: marshal context: %w", err)
		}
		pbReqs[i] = &flagspb.ResolveBooleanRequest{
			Key:          r.Key,
			ContextJson:  ctxJSON,
			DefaultValue: r.DefaultValue,
		}
	}
	resp, err := c.stub.ResolveBatch(c.authCtx(ctx), &flagspb.ResolveBatchRequest{Requests: pbReqs})
	if err != nil {
		return nil, fmt.Errorf("flagz: ResolveBatch: %w", err)
	}
	results := make([]flagz.EvaluateResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = flagz.EvaluateResult{Key: r.Key, Value: r.Value}
	}
	return results, nil
}

// -- Streamer ----------------------------------------------------------------

// Stream connects to the WatchFlag gRPC stream and emits FlagEvents on the returned channel.
// The channel is closed when ctx is cancelled or the stream ends.
func (c *Client) Stream(ctx context.Context, lastEventID int64) (<-chan flagz.FlagEvent, error) {
	stream, err := c.stub.WatchFlag(c.authCtx(ctx), &flagspb.WatchFlagRequest{
		LastEventId: lastEventID,
	})
	if err != nil {
		return nil, fmt.Errorf("flagz: WatchFlag: %w", err)
	}

	ch := make(chan flagz.FlagEvent, 16)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			fe := flagz.FlagEvent{EventID: ev.EventId, Key: ev.Key}
			switch ev.Type {
			case flagspb.WatchFlagEventType_FLAG_UPDATED:
				fe.Type = "update"
			case flagspb.WatchFlagEventType_FLAG_DELETED:
				fe.Type = "delete"
			default:
				fe.Type = "unknown"
			}
			if ev.Flag != nil {
				f, err := protoToFlag(ev.Flag)
				if err == nil {
					fe.Flag = &f
				}
			}
			select {
			case ch <- fe:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
