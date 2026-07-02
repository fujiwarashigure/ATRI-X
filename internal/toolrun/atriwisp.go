package toolrun

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
)

var atriwispToolNameReplacer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

type AtriwispToolDeclaration struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Schema         map[string]any `json:"schema,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	Endpoint       string         `json:"endpoint,omitempty"`
	ForegroundOnly bool           `json:"foreground_only,omitempty"`
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
		canonical := atriwispToolName(atriwispName, name)
		out = append(out, CachedTool{
			Name:           name,
			CanonicalName:  canonical,
			Source:         SourceKindAtriwisp,
			Description:    declared.Description,
			Schema:         llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{Name: canonical, Description: declared.Description, Parameters: declared.Schema}},
			AtriwispName:     atriwispName,
			EventKey:       injection.EventKey,
			Endpoint:       strings.TrimSpace(declared.Endpoint),
			TimeoutSeconds: declared.TimeoutSeconds,
			ForegroundOnly: declared.ForegroundOnly,
		})
	}
	return NormalizeCachedTools(out)
}

func atriwispToolName(atriwispName, name string) string {
	atriwispName = atriwispToolNameReplacer.ReplaceAllString(strings.TrimSpace(atriwispName), "_")
	name = atriwispToolNameReplacer.ReplaceAllString(strings.TrimSpace(name), "_")
	return fmt.Sprintf("atriwisp_%s_%s", atriwispName, name)
}
