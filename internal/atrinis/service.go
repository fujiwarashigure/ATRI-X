package atrinis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fujiwarashigure/ATRI-X/internal/background"
	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/atriena"
	"github.com/fujiwarashigure/ATRI-X/internal/media"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
)

type SenderFunc func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error)

type AuditFunc func(event string, attrs ...any)

type QueuedLLMEvent struct {
	Event   Event
	EventID string
}

type EnqueueLLMFunc func(ctx context.Context, event QueuedLLMEvent) error

type ModelResolverFunc func(slot string) config.ModelSelection

type Options struct {
	Media              *media.Manager
	MediaRetentionDays int
	Config             config.AtrinisConfig
	SandboxRoot        string
	Tokens             map[string]string
	Store              storage.Store
	Logger             *slog.Logger
	Audit              AuditFunc
	Send               SenderFunc
	Runner             background.Runner
	ResolveModel       ModelResolverFunc
	EnabledPlatforms   []string
	PlatformCallers    atriena.PlatformCallerResolver
}

type Service struct {
	media            *media.Manager
	retentionDays    int
	cfg              config.AtrinisConfig
	sandboxRoot      string
	tokens           map[string]string
	store            storage.Store
	logger           *slog.Logger
	audit            AuditFunc
	send             SenderFunc
	runner           background.Runner
	resolveModel     ModelResolverFunc
	enabledPlatforms []string
	platformCallers  atriena.PlatformCallerResolver
	enqueueLLM       EnqueueLLMFunc
}

func NewService(opts Options) (*Service, error) {
	if opts.SandboxRoot == "" {
		opts.SandboxRoot = filepath.Join("data", "sandbox")
	}
	if opts.Store == nil || opts.Store.AtrinisEvents() == nil {
		return nil, fmt.Errorf("atrinis event store is not configured")
	}
	if opts.Config.Enabled && len(opts.Tokens) == 0 {
		return nil, fmt.Errorf("atrinis enabled but no tokens are configured")
	}
	return &Service{
		media:            opts.Media,
		retentionDays:    opts.MediaRetentionDays,
		cfg:              opts.Config,
		sandboxRoot:      opts.SandboxRoot,
		tokens:           opts.Tokens,
		store:            opts.Store,
		logger:           opts.Logger,
		audit:            opts.Audit,
		send:             opts.Send,
		runner:           opts.Runner,
		resolveModel:     opts.ResolveModel,
		enabledPlatforms: uniqueSorted(opts.EnabledPlatforms),
		platformCallers:  opts.PlatformCallers,
	}, nil
}

func (s *Service) SetLLMEnqueuer(enqueue EnqueueLLMFunc) {
	s.enqueueLLM = enqueue
}

func (s *Service) Handle(ctx context.Context, token string, req Request) (Response, error) {
	tokenName, ok := s.authenticate(token)
	if !ok {
		s.auditEvent("atrinis.auth_failed")
		return Response{Accepted: false, Status: StatusFailed, Error: "unauthorized"}, fmt.Errorf("unauthorized")
	}
	return s.DispatchAtriena(ctx, atriena.Origin{Kind: atriena.OriginHTTPToken, Name: tokenName}, req)
}

func (s *Service) DispatchAtriena(ctx context.Context, origin atriena.Origin, req Request) (Response, error) {
	if err := origin.Validate(); err != nil {
		return Response{Accepted: false, Status: StatusFailed, Error: err.Error()}, err
	}
	event, err := s.prepareEvent(origin, req)
	if err != nil {
		s.auditEvent("atrinis.rejected", "origin", origin.Label(), "error", err.Error())
		return Response{Accepted: false, Status: StatusFailed, Error: err.Error()}, err
	}
	return s.handlePreparedEvent(ctx, event)
}

func (s *Service) handlePreparedEvent(ctx context.Context, event Event) (Response, error) {
	req := event.Request
	attrs := s.eventAttrs(event)
	if err := s.authorizeAtriwisp(event); err != nil {
		s.auditEvent("atrinis.permission_denied", append(attrs, "error", err.Error())...)
		s.logWarn("atrinis permission denied", append(attrs, "error", err.Error())...)
		return Response{Accepted: false, EventKey: event.EventKey, Mode: req.Mode, Status: StatusFailed, Error: err.Error()}, err
	}
	if err := s.authorizeInternalTools(event); err != nil {
		s.auditEvent("atrinis.tool_denied", append(attrs, "error", err.Error())...)
		s.logWarn("atrinis internal tool denied", append(attrs, "error", err.Error())...)
		return Response{Accepted: false, EventKey: event.EventKey, Mode: req.Mode, Status: StatusFailed, Error: err.Error()}, err
	}
	if err := s.authorizeExternalTools(event); err != nil {
		s.auditEvent("atrinis.external_tool_denied", append(attrs, "error", err.Error())...)
		s.logWarn("atrinis external tool denied", append(attrs, "error", err.Error())...)
		return Response{Accepted: false, EventKey: event.EventKey, Mode: req.Mode, Status: StatusFailed, Error: err.Error()}, err
	}
	if existing, err := s.store.AtrinisEvents().GetByKey(ctx, req.Atriwisp.Name, req.Source, req.ID); err == nil {
		s.handleDuplicate(event, existing)
		return Response{Accepted: true, Duplicate: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusDuplicate}, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return Response{}, err
	}

	status := StatusAccepted
	if req.Mode == ModeLLM {
		status = StatusQueued
	}
	var mediaIDs []string
	if req.Mode != ModeRecord {
		for _, seg := range req.Segments {
			if media.ValidID(seg.URL) {
				mediaIDs = append(mediaIDs, seg.URL)
			}
		}
	}
	record, err := s.store.AtrinisEvents().Create(ctx, storage.CreateAtrinisEventRequest{
		MediaIDs:         mediaIDs,
		EventKey:         event.EventKey,
		TokenName:        event.Origin.Label(),
		AtriwispName:       req.Atriwisp.Name,
		Source:           req.Source,
		SourceID:         req.ID,
		Tags:             event.TagsJSON,
		Mode:             req.Mode,
		ModelSlot:        req.ModelSlot,
		ContentHash:      event.ContentHash,
		ToolDeclarations: event.ToolDeclarations,
		ToolHash:         event.ToolHash,
		RequestedTargets: event.RequestedTargets,
		ResolvedTargets:  event.ResolvedTargets,
		Status:           status,
		ReceivedAt:       event.ReceivedAt,
		CreatedAt:        event.CreatedAt,
	})
	if err != nil {
		return Response{}, err
	}
	attrs = append(attrs, "event_id", record.ID)
	s.auditEvent("atrinis.accepted", attrs...)
	s.logInfo("atrinis event accepted", attrs...)

	switch req.Mode {
	case ModeRecord:
		if err := s.completeEvent(ctx, record.ID, event.ResolvedTargets, StatusCompleted, "", ""); err != nil {
			return Response{}, err
		}
		s.auditEvent("atrinis.recorded", attrs...)
		return Response{Accepted: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusCompleted}, nil
	case ModeDirect:
		if err := s.runDirect(ctx, event, record.ID); err != nil {
			s.auditEvent("atrinis.direct_failed", append(attrs, "error", err.Error())...)
			s.logWarn("atrinis direct failed", append(attrs, "error", err.Error())...)
			_ = s.completeEvent(ctx, record.ID, event.ResolvedTargets, StatusFailed, "", err.Error())
			return Response{Accepted: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusFailed, Error: err.Error()}, err
		}
		s.auditEvent("atrinis.direct_completed", attrs...)
		return Response{Accepted: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusCompleted}, nil
	case ModeLLM:
		s.auditEvent("atrinis.llm_queued", attrs...)
		s.logInfo("atrinis llm queued", attrs...)
		if s.enqueueLLM != nil {
			if err := s.enqueueLLM(ctx, QueuedLLMEvent{Event: event, EventID: record.ID}); err != nil {
				_ = s.completeEvent(ctx, record.ID, event.ResolvedTargets, StatusFailed, "", err.Error())
				s.logWarn("atrinis llm enqueue failed", append(attrs, "error", err.Error())...)
				return Response{Accepted: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusFailed, Error: err.Error()}, err
			}
		}
		return Response{Accepted: true, EventKey: event.EventKey, Mode: req.Mode, Status: StatusQueued}, nil
	default:
		return Response{}, fmt.Errorf("unsupported mode %q", req.Mode)
	}
}
