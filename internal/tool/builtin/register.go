package builtin

import (
	"context"

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
	FileManager         *FileManager
}

func RegisterAll(registry *tool.Registry, opts RegisterOptions) error {
	var beforeDiscover func(context.Context) error
	if opts.SkillManager != nil {
		beforeDiscover = opts.SkillManager.EnsureLoaded
	}
	if err := registry.Register(tool.NewDiscoverTool(registry, beforeDiscover)); err != nil {
		return err
	}
	if err := registry.Register(NewWorkspaceTool()); err != nil {
		return err
	}
	if opts.ResidentMemoryStore != nil {
		for _, memoryTool := range NewResidentMemoryTools(opts.ResidentMemoryStore) {
			if err := registry.Register(memoryTool); err != nil {
				return err
			}
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
	if opts.FileManager != nil {
		if err := registry.Register(NewSendFileTool(opts.FileManager)); err != nil {
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
	fileGuard := NewFileGuard()
	if opts.SkillManager != nil {
		fileGuard.AddRule(NewAtrixSkillFileGuardRule(opts.SkillManager.Root))
	}
	if opts.ResidentMemoryStore != nil {
		fileGuard.AddRule(NewResidentMemoryFileGuardRule(opts.ResidentMemoryStore.Path))
	}
	if opts.LongMemoryDir != "" {
		fileGuard.AddRule(NewLongMemoryFileGuardRule(opts.LongMemoryDir))
	}
	if err := registry.Register(NewReadFileTool(fileGuard)); err != nil {
		return err
	}
	if err := registry.Register(NewEditFileTool(fileGuard)); err != nil {
		return err
	}
	if err := registry.Register(NewShellTool(fileGuard)); err != nil {
		return err
	}
	if err := registry.Register(NewAtriwispCreatorTool()); err != nil {
		return err
	}
	catalog := (*skill.Catalog)(nil)
	if opts.SkillManager != nil {
		catalog = opts.SkillManager.Catalog
	}
	if err := registry.Register(skill.NewAgentSkillTool(opts.SkillManager)); err != nil {
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
		if err := registry.Register(skill.NewFinalizeAtrixSkillTool(opts.SkillManager)); err != nil {
			return err
		}
	}

	return nil
}
