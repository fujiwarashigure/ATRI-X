package agent

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/completion"
)

// CompletionService exposes the structured completion service for platform adapters.
func (a *Agent) CompletionService() *completion.Service {
	return a.completion
}

// Complete returns command completions for legacy platform adapters and tests.
func (a *Agent) Complete(text string) []string {
	return completion.Texts(a.completion.Complete(context.Background(), completion.Request{Text: text}))
}
