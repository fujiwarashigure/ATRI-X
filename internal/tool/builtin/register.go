package builtin

import (
	elcron "github.com/fujiwarashigure/ATRI-X/internal/cron"
	"github.com/fujiwarashigure/ATRI-X/internal/memory/resident"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
	"github.com/fujiwarashigure/ATRI-X/internal/tool/skill"
)

type RegisterOptions struct {
	ResidentMemoryStore *resident.Store
	SkillManager        *skill.Manager
	CronService         *elcron.Service
	ChatHistory         storage.ChatHistoryRepository
	LongMemoryDir       string
	ArtifactManager     *ArtifactManager
}

func RegisterAll(registry *tool.Registry, opts RegisterOptions) error {
	if err := registry.Register(tool.NewDiscoverTool(registry)); err != nil {
		return err
	}
	if opts.ResidentMemoryStore != nil {
		if err := registry.Register(NewResidentMemoryTool(opts.ResidentMemoryStore)); err != nil {
			return err
		}
	}
	if longMemoryDir := opts.LongMemoryDir; longMemoryDir != "" {
		for _, memoryTool := range NewLongMemoryTools(longMemoryDir) {
			if err := registry.Register(memoryTool); err != nil {
				return err
			}
		}
	}
	if opts.CronService != nil {

		for _, cronTool := range NewCronTools(opts.CronService) {
			if err := registry.Register(cronTool); err != nil {
				return err
			}
		}
	}
	if opts.ArtifactManager != nil {
		if err := registry.Register(NewSendFileTool(opts.ArtifactManager)); err != nil {
			return err
		}
	}
	if opts.ChatHistory != nil {
		if err := registry.Register(NewSearchChatHistoryTool(opts.ChatHistory)); err != nil {
			return err
		}
		if err := registry.Register(NewGetChatHistoryAroundTool(opts.ChatHistory)); err != nil {
			return err
		}
		if err := registry.Register(NewReplyToChatHistoryMessageTool(opts.ChatHistory)); err != nil {
			return err
		}
	}
	if err := registry.Register(NewWebSearchTool()); err != nil {
		return err
	}
	if err := registry.Register(NewWebExtractTool()); err != nil {
		return err
	}
	if err := registry.Register(NewShellTool()); err != nil {
		return err
	}
	catalog := (*skill.Catalog)(nil)
	if opts.SkillManager != nil {
		catalog = opts.SkillManager.Catalog
	}
	if err := registry.Register(skill.NewPythonRunner(catalog)); err != nil {
		return err
	}
	if err := registry.Register(skill.NewGoRunner(catalog)); err != nil {
		return err
	}
	if opts.SkillManager != nil {
		if err := registry.Register(skill.NewCreateAtrixSkillTool(opts.SkillManager)); err != nil {
			return err
		}
		if err := registry.Register(skill.NewReadAtrixSkillTool(opts.SkillManager)); err != nil {
			return err
		}
		if err := registry.Register(skill.NewModifyAtrixSkillTool(opts.SkillManager)); err != nil {
			return err
		}
		if err := registry.Register(skill.NewReadGoSkillTool(opts.SkillManager)); err != nil {
			return err
		}
		if err := registry.Register(skill.NewModifyGoSkillTool(opts.SkillManager)); err != nil {
			return err
		}
	}

	return nil
}
