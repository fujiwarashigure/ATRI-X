package skill

import (
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

func appendAtriyphWarnings(content string, diagnostics []atriyph.Diagnostic) string {
	warnings := atriyph.WarningDiagnostics(diagnostics)
	if len(warnings) == 0 {
		return content
	}
	return tool.AppendWarnings(content, []string{strings.TrimSpace(atriyph.FormatDiagnostics(warnings))})
}
