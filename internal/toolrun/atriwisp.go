package toolrun

import (
	"fmt"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
)

type AtriwispToolDeclaration struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Schema         map[string]any `json:"schema,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	Endpoint       string         `json:"endpoint,omitempty"`
}

type AtriwispInjection struct {
	AtriwispName string
	EventKey   string
	Tools      []AtriwispToolDeclaration
}

func CachedToolsFromAtriwisp(injection AtriwispInjection) []CachedTool {
	atriwispName := strings.TrimSpace(injection.AtriwispName)
	if atriwispName == "" {
		return nil
	}
	out := make([]CachedTool, 0, len(injection.Tools))
	for _, declared := range injection.Tools {
		name := strings.TrimSpace(declared.Name)
		if name == "" {
			continue
		}
		canonical := fmt.Sprintf("atriwisp.%s.%s", atriwispName, name)
		out = append(out, CachedTool{
			Name:          name,
			CanonicalName: canonical,
			Source:        SourceKindAtriwisp,
			Description:   declared.Description,
			Schema: llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{
				Name:        canonical,
				Description: declared.Description,
				Parameters:  declared.Schema,
			}},
			AtriwispName: atriwispName,
			EventKey:   injection.EventKey,
		})
	}
	return NormalizeCachedTools(out)
}
