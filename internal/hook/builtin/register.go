package builtin

import (
	"log/slog"

	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	"github.com/fujiwarashigure/ATRI-X/internal/hook/plugins/emoticon"
	residentmemory "github.com/fujiwarashigure/ATRI-X/internal/hook/plugins/resident_memory"
	"github.com/fujiwarashigure/ATRI-X/internal/hook/rules"
	"github.com/fujiwarashigure/ATRI-X/internal/memory/resident"
	"github.com/fujiwarashigure/ATRI-X/internal/security"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

// Options contains shared dependencies for hook plugins shipped with ATRI-X.
type Options struct {
	ConfigDir           string
	Tools               *tool.Registry
	Policy              *security.Policy
	ResidentMemoryStore *resident.Store
	Logger              *slog.Logger
	Audit               func(event string, attrs ...any)
}

func RegisterAll(registrar hook.Registrar, opts Options) error {
	if registrar == nil {
		return nil
	}
	rulesModule, err := rules.NewModule(rules.Options{
		ConfigDir: opts.ConfigDir,
		Tools:     opts.Tools,
		Policy:    opts.Policy,
		Logger:    opts.Logger,
		Audit:     opts.Audit,
	})
	if err != nil {
		return err
	}
	emoticonModule, err := emoticon.NewModule(emoticon.Options{ConfigDir: opts.ConfigDir, Logger: opts.Logger})
	if err != nil {
		return err
	}
	residentMemoryModule := residentmemory.NewModule(residentmemory.Options{Store: opts.ResidentMemoryStore})
	modules := []hook.Module{
		residentMemoryModule,
		rulesModule,
		emoticonModule,
	}
	for _, module := range modules {
		if err := module.RegisterHooks(registrar); err != nil {
			return err
		}
	}
	return nil
}
