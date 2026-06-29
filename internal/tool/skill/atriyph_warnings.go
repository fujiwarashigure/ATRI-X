package skill

import (
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
)

func appendAtriyphWarnings(content string, diagnostics []atriyph.Diagnostic) string {
	warnings := atriyph.WarningDiagnostics(diagnostics)
	if len(warnings) == 0 {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n\nWarnings:\n" + atriyph.FormatDiagnostics(warnings)
}
