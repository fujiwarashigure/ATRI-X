package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/atrinis"
)

type defaultIntegrationFactory struct{}

func (defaultIntegrationFactory) Attach(ctx context.Context, req IntegrationRequest) (PlatformComponents, error) {
	foundation := req.Foundation
	runtime := req.Runtime
	platforms := req.Platforms
	cfg := foundation.Config
	if runtime.Agent == nil {
		return PlatformComponents{}, fmt.Errorf("app: integration runtime has no agent")
	}

	if cfg.Atrinis.Enabled && req.Mode != RunModeCLIOnly {
		if runtime.AtrienaBus == nil {
			return PlatformComponents{}, fmt.Errorf("app: integration runtime has no atriena bus")
		}
		atrinisTokens, err := resolveAtrinisTokens(cfg)
		if err != nil {
			return PlatformComponents{}, err
		}
		atrinisService, err := atrinis.NewService(atrinis.Options{
			Media:            runtime.Media,
			Config:           cfg.Atrinis,
			SandboxRoot:      cfg.Sandbox.Root,
			Tokens:           atrinisTokens,
			Store:            foundation.Store,
			Logger:           foundation.Logs.Atrinis(),
			EnabledPlatforms: runtimeNames(platforms.Runtimes),
			PlatformCallers:  platformCallerResolver{runtimes: platforms.Runtimes},
			Audit:            auditFunc(foundation.Logs),
			Send: func(ctx context.Context, target delivery.Target, outputs []delivery.Output) (delivery.Receipt, error) {
				return runtime.Agent.SendNotice(ctx, delivery.Notice{Target: target, Outputs: outputs})
			},
			Runner: runtime.Agent,
			ResolveModel: func(slot string) config.ModelSelection {
				selected := runtime.Agent.CurrentModelForMode(slot)
				return config.ModelSelection{Provider: selected.Provider, Model: selected.Model}
			},
		})
		if err != nil {
			return PlatformComponents{}, err
		}
		runtime.AtrienaBus.SetDispatcher(atrinisService)
		platforms.Runtimes = append(platforms.Runtimes, atrinisRuntimeAdapter{runtime: atrinis.NewRuntime(cfg.Atrinis.HTTP, atrinisService)})
		req.Profiler.Mark("atrinis init")
	}

	registerCompletionPlatforms(runtime.Agent, platforms.Runtimes)
	registerCommandCatalogs(runtime.Agent, platforms.Runtimes)
	registerPlatformHooks(runtime.Agent, platforms.Runtimes, req.Health)
	req.Profiler.Mark("platform hooks")
	// 适配器启动前先订阅：平台连接事实走全局信号，订阅方必须在第一个 Emit 之前就位，
	// 否则那次连接会被丢掉（信号不缓存、不重放）。
	if err := runtime.Agent.StartPlatformEvents(ctx); err != nil {
		return PlatformComponents{}, fmt.Errorf("app: start platform event subscription: %w", err)
	}
	req.Profiler.Mark("platform events")
	return platforms, nil
}

func resolveAtrinisTokens(cfg *config.Config) (map[string]string, error) {
	out := map[string]string{}
	configDir := filepath.Dir(cfg.ConfigPath)
	for name, tokenCfg := range cfg.Atrinis.Tokens {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, envName := range tokenCfg.TokenEnv {
			envName = strings.TrimSpace(envName)
			if envName == "" {
				continue
			}
			value, ok, err := config.ConfigEnv(envName, configDir)
			if err != nil {
				return nil, fmt.Errorf("resolve atrinis token %q from %s: %w", name, envName, err)
			}
			if ok && strings.TrimSpace(value) != "" {
				out[name] = strings.TrimSpace(value)
				break
			}
		}
	}
	return out, nil
}

func runtimeNames(runtimes []platformRuntime) []string {
	out := []string{}
	for _, runtime := range runtimes {
		if runtime == nil {
			continue
		}
		name := strings.TrimSpace(runtime.Name())
		if name != "" && name != "atrinis" {
			out = append(out, name)
		}
	}
	return out
}
