package atrinis

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriena"
	"github.com/fujiwarashigure/ATRI-X/internal/toolrun"
)

func (s *Service) authenticate(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	for name, value := range s.tokens {
		if value != "" && token == value {
			return name, true
		}
	}
	return "", false
}

func (s *Service) authorizeAtriwisp(event Event) error {
	policy, ok := s.cfg.Atriwisps[event.Request.Atriwisp.Name]
	if !ok {
		return nil
	}
	if policy.Enabled != nil && !*policy.Enabled {
		return fmt.Errorf("atriwisp %q is disabled", event.Request.Atriwisp.Name)
	}
	allowedTokens := trimStrings(policy.AllowedTokens)
	if len(allowedTokens) == 0 {
		return nil
	}
	if event.Origin.Kind != atriena.OriginHTTPToken {
		return nil
	}
	for _, token := range allowedTokens {
		if token == event.Origin.Name {
			return nil
		}
	}
	return fmt.Errorf("token %q is not allowed for atriwisp %q", event.Origin.Name, event.Request.Atriwisp.Name)
}

func (s *Service) authorizeInternalTools(event Event) error {
	allowed := s.allowedInternalTools(event.Request.Atriwisp.Name)
	for _, name := range backgroundToolNames(event.Request.ToolListNames) {
		if name == "discover_tool" {
			continue
		}
		if !allowed[name] {
			return fmt.Errorf("tool %q is not allowed for atriwisp %q", name, event.Request.Atriwisp.Name)
		}
	}
	return nil
}

func (s *Service) allowedInternalTools(atriwispName string) map[string]bool {
	allowedTools := s.cfg.AllowedTools
	if policy, ok := s.cfg.Atriwisps[atriwispName]; ok && policy.AllowedTools != nil {
		allowedTools = policy.AllowedTools
	}
	return setFromStrings(allowedTools)
}

func (s *Service) authorizeExternalTools(event Event) error {
	disabled := map[string]bool{}
	if policy, ok := s.cfg.Atriwisps[event.Request.Atriwisp.Name]; ok {
		disabled = setFromStrings(policy.DisabledExternalTools)
	}
	seen := map[string]bool{}
	for _, declared := range event.Request.Tools {
		name := strings.TrimSpace(declared.Name)
		if name == "" {
			return fmt.Errorf("external tool name is required")
		}
		if seen[name] {
			return fmt.Errorf("external tool %q is duplicated", name)
		}
		seen[name] = true
		if disabled[name] {
			return fmt.Errorf("external tool %q is disabled for atriwisp %q", name, event.Request.Atriwisp.Name)
		}
		if strings.ContainsAny(name, ". /\\") {
			return fmt.Errorf("external tool %q has invalid name", name)
		}
		if strings.TrimSpace(declared.Endpoint) == "" {
			return fmt.Errorf("external tool %q endpoint is required", name)
		}
		parsed, err := url.Parse(strings.TrimSpace(declared.Endpoint))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("external tool %q endpoint must be http or https URL", name)
		}
		if declared.TimeoutSeconds < 0 || declared.TimeoutSeconds > 60 {
			return fmt.Errorf("external tool %q timeout_seconds must be between 0 and 60", name)
		}
		if len(declared.Schema) == 0 {
			return fmt.Errorf("external tool %q schema is required", name)
		}
		if schemaType, _ := declared.Schema["type"].(string); schemaType != "object" {
			return fmt.Errorf("external tool %q schema.type must be object", name)
		}
	}
	return nil
}

func (s *Service) atriwispCachedTools(event Event) []toolrun.CachedTool {
	return toolrun.CachedToolsFromAtriwisp(toolrun.AtriwispInjection{AtriwispName: event.Request.Atriwisp.Name, EventKey: event.EventKey, Tools: event.Request.Tools})
}
