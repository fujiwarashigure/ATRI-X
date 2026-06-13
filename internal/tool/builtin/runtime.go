package builtin

import (
	"fmt"
	"path/filepath"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	elcron "github.com/fujiwarashigure/ATRI-X/internal/cron"
	"github.com/fujiwarashigure/ATRI-X/internal/memory/resident"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
	"github.com/fujiwarashigure/ATRI-X/internal/tool/skill"
)

type Runtime struct {
	Registry            *tool.Registry
	ResidentMemoryStore *resident.Store
	SkillManager        *skill.Manager
	ArtifactManager     *ArtifactManager
}

type RuntimeOptions struct {
	ConfigDir      string
	CronService    *elcron.Service
	ChatHistory    storage.ChatHistoryRepository
	SandboxRoot    string
	ArtifactConfig config.ArtifactConfig
}

func NewRuntime(opts RuntimeOptions) (*Runtime, error) {
	if opts.ConfigDir == "" {
		return nil, fmt.Errorf("builtin runtime config dir is required")
	}
	registry := tool.NewRegistry()
	residentStore := resident.NewStore(filepath.Join(opts.ConfigDir, "memories.toml"))
	skillManager := skill.NewManager(filepath.Join(opts.ConfigDir, "skills"), registry)
	artifactManager := NewArtifactManager(opts.SandboxRoot, opts.ArtifactConfig)
	runtime := &Runtime{Registry: registry, ResidentMemoryStore: residentStore, SkillManager: skillManager, ArtifactManager: artifactManager}
	if err := RegisterAll(registry, RegisterOptions{
		ResidentMemoryStore: residentStore,
		SkillManager:        skillManager,
		CronService:         opts.CronService,
		ChatHistory:         opts.ChatHistory,
		LongMemoryDir:       filepath.Join(opts.ConfigDir, "long_memory"),
		ArtifactManager:     artifactManager,
	}); err != nil {
		return nil, err
	}
	return runtime, nil
}
