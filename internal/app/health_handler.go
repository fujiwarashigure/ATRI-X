package app

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/health"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
)

type healthHandler struct {
	inner platform.PlatformHandler
	state *health.State
}

func (h healthHandler) HandleMessage(ctx context.Context, text string) error {
	if h.inner == nil {
		return nil
	}
	err := h.inner.HandleMessage(ctx, text)
	if h.state != nil {
		if err != nil {
			h.state.RecordMessageError()
		} else {
			h.state.RecordMessageHandled()
		}
	}
	return err
}
