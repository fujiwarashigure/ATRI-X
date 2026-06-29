package atrinis

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/security"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
)

func isAtrinisModelSlot(slot string) bool {
	switch strings.TrimSpace(slot) {
	case "atriwisp1", "atriwisp2", "atriwisp3":
		return true
	default:
		return false
	}
}

func isAtrinisSessionMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case storage.SessionModeWork, storage.SessionModeChat:
		return true
	default:
		return false
	}
}

func atrinisActor(event Event) security.Actor {
	id := security.ActorID("atrinis", event.Origin.Label())
	return security.Actor{ID: id, Platform: "atrinis", PlatformUserID: event.Origin.Label(), DisplayName: event.Request.Atriwisp.Name, Role: security.RoleSuperadmin}
}

func firstPlatform(rawTargets string) string {
	var targets []Target
	if err := json.Unmarshal([]byte(rawTargets), &targets); err != nil {
		return ""
	}
	for _, target := range targets {
		if strings.TrimSpace(target.Platform) != "" {
			return target.Platform
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range trimStrings(values) {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func atrinisSandboxSubdir(atriwispName string) string {
	return filepath.ToSlash(filepath.Join("atrinis", strings.TrimSpace(atriwispName)))
}
