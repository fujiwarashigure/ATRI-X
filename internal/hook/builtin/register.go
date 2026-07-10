package builtin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	residentmemory "github.com/fujiwarashigure/ATRI-X/internal/hook/plugins/resident_memory"
	"github.com/fujiwarashigure/ATRI-X/internal/hook/rules"
	hookruntime "github.com/fujiwarashigure/ATRI-X/internal/hook/runtime"
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
	Notify              func(context.Context, string)
	Send                func(context.Context, delivery.Target, delivery.Output) (delivery.Receipt, error)
	PlatformCallers     rules.PlatformCallerResolver
	Runtime             *hookruntime.Manager
}

func RegisterAll(registrar hook.Registrar, opts Options) error {
	if registrar == nil {
		return nil
	}
	residentMemoryModule := residentmemory.NewModule(residentmemory.Options{Store: opts.ResidentMemoryStore})
	registerModule(registrar, opts, "resident_memory", residentMemoryModule)

	rulesModule, err := rules.NewModule(rules.Options{
		ConfigDir:       opts.ConfigDir,
		Tools:           opts.Tools,
		Policy:          opts.Policy,
		Logger:          opts.Logger,
		Audit:           opts.Audit,
		Notify:          opts.Notify,
		Send:            opts.Send,
		PlatformCallers: opts.PlatformCallers,
		Runtime:         opts.Runtime,
	})
	if err == nil {
		registerModule(registrar, opts, "rules", rulesModule)
	} else {
		reportPluginError(opts, "rules", err)
	}
	return nil
}

func registerModule(registrar hook.Registrar, opts Options, name string, module hook.Module) {
	if err := module.RegisterHooks(registrar); err != nil {
		reportPluginError(opts, name, err)
	}
}

func reportPluginError(opts Options, name string, err error) {
	if err == nil {
		return
	}
	if opts.Logger != nil {
		opts.Logger.Error("hook plugin disabled", "plugin", name, "error", err)
	}
	if opts.Notify != nil {
		opts.Notify(context.Background(), fmt.Sprintf("Hook 插件 %s 已禁用：%v", name, err))
	}
}
