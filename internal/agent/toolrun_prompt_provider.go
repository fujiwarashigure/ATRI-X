package agent

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/session"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/toolrun"
)

type toolRunPromptProvider struct {
	agent *Agent
}

func (p toolRunPromptProvider) Schemas(ctx context.Context, mode string, session *storage.Session, scope session.Scope) ([]llm.ToolSchema, error) {
	if p.agent == nil || session == nil {
		return nil, nil
	}
	return p.agent.toolRunManager().BaseSchemas(ctx, toolrun.Context{Mode: mode, Session: session, Scope: scope, Actor: p.agent.actor(ctx), DisableBaseTools: isBackgroundSession(session)})
}

func (p toolRunPromptProvider) ToolNames(ctx context.Context, mode string, session *storage.Session, scope session.Scope) ([]string, error) {
	if p.agent == nil || session == nil {
		return nil, nil
	}
	return p.agent.toolRunManager().ToolNames(ctx, toolrun.Context{Mode: mode, Session: session, Scope: scope, Actor: p.agent.actor(ctx), DisableBaseTools: isBackgroundSession(session)})
}
