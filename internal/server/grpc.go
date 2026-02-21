package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	flagspb "github.com/matt-riley/flagz/api/proto/v1"
	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultGRPCStreamPollInterval = time.Second

// GRPCServer implements the FlagService gRPC interface, providing flag CRUD,
// boolean evaluation, batch resolution, and server-streaming watch.
type GRPCServer struct {
	flagspb.UnimplementedFlagServiceServer
	service            Service
	streamPollInterval time.Duration
}

// NewGRPCServer creates a [GRPCServer] with a default stream poll interval of
// 1 second.
func NewGRPCServer(svc Service) *GRPCServer {
	return NewGRPCServerWithStreamPollInterval(svc, defaultGRPCStreamPollInterval)
}

// NewGRPCServerWithStreamPollInterval creates a [GRPCServer] with the specified
// poll interval for the WatchFlag streaming RPC.
func NewGRPCServerWithStreamPollInterval(svc Service, streamPollInterval time.Duration) *GRPCServer {
	if svc == nil {
		panic("service is nil")
	}

	if streamPollInterval <= 0 {
		streamPollInterval = defaultGRPCStreamPollInterval
	}

	return &GRPCServer{
		service:            svc,
		streamPollInterval: streamPollInterval,
	}
}

func (s *GRPCServer) CreateFlag(ctx context.Context, req *flagspb.CreateFlagRequest) (*flagspb.CreateFlagResponse, error) {
	if req == nil || req.GetFlag() == nil {
		return nil, status.Error(codes.InvalidArgument, "flag is required")
	}
	if strings.TrimSpace(req.GetFlag().GetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	created, err := s.service.CreateFlag(ctx, protoFlagToRepository(req.GetFlag()))
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &flagspb.CreateFlagResponse{Flag: repositoryFlagToProto(created)}, nil
}

func (s *GRPCServer) UpdateFlag(ctx context.Context, req *flagspb.UpdateFlagRequest) (*flagspb.UpdateFlagResponse, error) {
	if req == nil || req.GetFlag() == nil {
		return nil, status.Error(codes.InvalidArgument, "flag is required")
	}
	if strings.TrimSpace(req.GetFlag().GetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	updated, err := s.service.UpdateFlag(ctx, protoFlagToRepository(req.GetFlag()))
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &flagspb.UpdateFlagResponse{Flag: repositoryFlagToProto(updated)}, nil
}

func (s *GRPCServer) GetFlag(ctx context.Context, req *flagspb.GetFlagRequest) (*flagspb.GetFlagResponse, error) {
	if req == nil || strings.TrimSpace(req.GetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	flag, err := s.service.GetFlag(ctx, req.GetKey())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &flagspb.GetFlagResponse{Flag: repositoryFlagToProto(flag)}, nil
}

func (s *GRPCServer) ListFlags(ctx context.Context, req *flagspb.ListFlagsRequest) (*flagspb.ListFlagsResponse, error) {
	flags, err := s.service.ListFlags(ctx)
	if err != nil {
		return nil, toGRPCError(err)
	}

	pageSize := 0
	pageToken := ""
	if req != nil {
		pageSize = int(req.GetPageSize())
		pageToken = req.GetPageToken()
	}
	if pageSize < 0 {
		return nil, status.Error(codes.InvalidArgument, "page_size must be non-negative")
	}

	pageStart, err := parseListPageToken(pageToken, len(flags))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid page_token")
	}

	pageEnd := len(flags)
	nextPageToken := ""
	if pageSize > 0 {
		pageEnd = pageStart + pageSize
		if pageEnd > len(flags) {
			pageEnd = len(flags)
		}
		if pageEnd < len(flags) {
			nextPageToken = strconv.Itoa(pageEnd)
		}
	}

	flags = flags[pageStart:pageEnd]

	protoFlags := make([]*flagspb.Flag, 0, len(flags))
	for _, flag := range flags {
		protoFlags = append(protoFlags, repositoryFlagToProto(flag))
	}

	return &flagspb.ListFlagsResponse{
		Flags:         protoFlags,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *GRPCServer) DeleteFlag(ctx context.Context, req *flagspb.DeleteFlagRequest) (*flagspb.DeleteFlagResponse, error) {
	if req == nil || strings.TrimSpace(req.GetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	if err := s.service.DeleteFlag(ctx, req.GetKey()); err != nil {
		return nil, toGRPCError(err)
	}

	return &flagspb.DeleteFlagResponse{}, nil
}

func (s *GRPCServer) ResolveBoolean(ctx context.Context, req *flagspb.ResolveBooleanRequest) (*flagspb.ResolveBooleanResponse, error) {
	if req == nil || strings.TrimSpace(req.GetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	evalContext, err := decodeEvaluationContext(req.GetContextJson())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid context_json")
	}

	value, err := s.service.ResolveBoolean(ctx, req.GetKey(), evalContext, req.GetDefaultValue())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &flagspb.ResolveBooleanResponse{
		Key:   req.GetKey(),
		Value: value,
	}, nil
}

func (s *GRPCServer) ResolveBatch(ctx context.Context, req *flagspb.ResolveBatchRequest) (*flagspb.ResolveBatchResponse, error) {
	if req == nil || len(req.GetRequests()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "requests are required")
	}

	requests := make([]service.ResolveRequest, 0, len(req.GetRequests()))
	for idx, request := range req.GetRequests() {
		if strings.TrimSpace(request.GetKey()) == "" {
			return nil, status.Errorf(codes.InvalidArgument, "requests[%d].key is required", idx)
		}

		evalContext, err := decodeEvaluationContext(request.GetContextJson())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "requests[%d].context_json is invalid", idx)
		}

		requests = append(requests, service.ResolveRequest{
			Key:          request.GetKey(),
			Context:      evalContext,
			DefaultValue: request.GetDefaultValue(),
		})
	}

	results, err := s.service.ResolveBatch(ctx, requests)
	if err != nil {
		return nil, toGRPCError(err)
	}

	protoResults := make([]*flagspb.ResolveBatchResult, 0, len(results))
	for _, result := range results {
		protoResults = append(protoResults, &flagspb.ResolveBatchResult{
			Key:   result.Key,
			Value: result.Value,
		})
	}

	return &flagspb.ResolveBatchResponse{Results: protoResults}, nil
}

func (s *GRPCServer) WatchFlag(req *flagspb.WatchFlagRequest, stream flagspb.FlagService_WatchFlagServer) error {
	filterKey := ""
	var lastEventID int64
	if req != nil {
		filterKey = strings.TrimSpace(req.GetKey())
		lastEventID = req.GetLastEventId()
	}
	if lastEventID < 0 {
		return status.Error(codes.InvalidArgument, "last_event_id must be non-negative")
	}

	listEventsSince := s.service.ListEventsSince
	if filterKey != "" {
		listEventsSince = func(ctx context.Context, eventID int64) ([]repository.FlagEvent, error) {
			return s.service.ListEventsSinceForKey(ctx, eventID, filterKey)
		}
	}

	sendEvents := func(ctx context.Context) error {
		events, err := listEventsSince(ctx, lastEventID)
		if err != nil {
			return toGRPCError(err)
		}

		for _, event := range events {
			lastEventID = event.EventID
			watchEvent, ok := repositoryEventToProto(event)
			if !ok {
				continue
			}

			if err := stream.Send(watchEvent); err != nil {
				return err
			}
		}

		return nil
	}

	if err := sendEvents(stream.Context()); err != nil {
		return err
	}

	ticker := time.NewTicker(s.streamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			if err := sendEvents(stream.Context()); err != nil {
				return err
			}
		}
	}
}

func toGRPCError(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := status.FromError(err); ok {
		return err
	}

	switch {
	case errors.Is(err, service.ErrInvalidRules):
		return status.Error(codes.InvalidArgument, "invalid rules")
	case errors.Is(err, service.ErrInvalidVariants):
		return status.Error(codes.InvalidArgument, "invalid variants")
	case isInvalidArgumentError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrFlagNotFound):
		return status.Error(codes.NotFound, "flag not found")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func isInvalidArgumentError(err error) bool {
	if err == nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(err.Error()), "flag key is required")
}

func parseListPageToken(pageToken string, maxOffset int) (int, error) {
	pageToken = strings.TrimSpace(pageToken)
	if pageToken == "" {
		return 0, nil
	}

	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 || offset > maxOffset {
		return 0, errors.New("invalid page token")
	}

	return offset, nil
}

func protoFlagToRepository(flag *flagspb.Flag) repository.Flag {
	if flag == nil {
		return repository.Flag{}
	}

	return repository.Flag{
		Key:         flag.GetKey(),
		Description: flag.GetDescription(),
		Enabled:     flag.GetEnabled(),
		Variants:    append(json.RawMessage(nil), flag.GetVariantsJson()...),
		Rules:       append(json.RawMessage(nil), flag.GetRulesJson()...),
	}
}

func repositoryFlagToProto(flag repository.Flag) *flagspb.Flag {
	return &flagspb.Flag{
		Key:          flag.Key,
		Description:  flag.Description,
		Enabled:      flag.Enabled,
		VariantsJson: append([]byte(nil), flag.Variants...),
		RulesJson:    append([]byte(nil), flag.Rules...),
	}
}

func decodeEvaluationContext(payload []byte) (core.EvaluationContext, error) {
	if len(payload) == 0 {
		return core.EvaluationContext{}, nil
	}

	var evalContext core.EvaluationContext
	if err := json.Unmarshal(payload, &evalContext); err != nil {
		return core.EvaluationContext{}, err
	}

	return evalContext, nil
}

func repositoryEventToProto(event repository.FlagEvent) (*flagspb.WatchFlagEvent, bool) {
	watchEventType, ok := toProtoWatchEventType(event.EventType)
	if !ok {
		return nil, false
	}

	watchEvent := &flagspb.WatchFlagEvent{
		Type:    watchEventType,
		Key:     event.FlagKey,
		EventId: event.EventID,
	}

	if len(event.Payload) > 0 {
		var flag repository.Flag
		if err := json.Unmarshal(event.Payload, &flag); err == nil && strings.TrimSpace(flag.Key) != "" {
			watchEvent.Flag = repositoryFlagToProto(flag)
		}
	}

	return watchEvent, true
}

func toProtoWatchEventType(eventType string) (flagspb.WatchFlagEventType, bool) {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "update", "updated":
		return flagspb.WatchFlagEventType_FLAG_UPDATED, true
	case "delete", "deleted":
		return flagspb.WatchFlagEventType_FLAG_DELETED, true
	default:
		return flagspb.WatchFlagEventType_WATCH_FLAG_EVENT_TYPE_UNSPECIFIED, false
	}
}
