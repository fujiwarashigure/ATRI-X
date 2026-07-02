package toolrun

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

func AvailableInContext(ctx context.Context, info tool.Info) bool {
	return tool.InfoAvailableInContext(ctx, info)
}

func unavailableReason(ctx context.Context, info tool.Info) string {
	if info.ForegroundOnly && tool.BackgroundContext(ctx) {
		return "tool is only available in foreground sessions"
	}
	return "tool is unavailable in this context"
}
