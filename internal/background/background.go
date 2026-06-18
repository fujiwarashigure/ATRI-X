package background

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/security"
	"github.com/fujiwarashigure/ATRI-X/internal/toolrun"
)

type Kind string

const (
	KindCron  Kind = "cron"
	KindAtrinis Kind = "atrinis"
)

type Runner interface {
	RunBackground(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
	Kind          Kind
	Name          string
	Title         string
	Platform      string
	Actor         security.Actor
	ScopeID       string
	SessionID     string
	ModelProvider string
	Model         string
	Prompt        string
	RetryPrompt   string
	ToolListNames []string
	CachedTools   []toolrun.CachedTool
	SandboxSubdir string
	Metadata      map[string]string
}

type RunResult struct {
	SessionID string
	Text      string
	Parsed    JSONResult
	ParseErr  error
}

type JSONResult struct {
	Completed  bool   `json:"completed"`
	NeedReport bool   `json:"need_report"`
	Report     string `json:"report"`
}
