package runtimeinfo

import (
	"path/filepath"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/config"
)

type Info struct {
	ConfigDir    string
	ConfigPath   string
	SandboxRoot  string
	FileDelivery config.FileDeliveryConfig
	Now          func() time.Time
}

func First(values ...Info) Info {
	if len(values) == 0 {
		return Info{}.Normalize()
	}
	return values[0].Normalize()
}

func (i Info) Normalize() Info {
	if i.ConfigDir == "" && i.ConfigPath != "" {
		i.ConfigDir = filepath.Dir(filepath.Clean(i.ConfigPath))
	}
	if i.ConfigPath == "" && i.ConfigDir != "" {
		i.ConfigPath = filepath.Join(i.ConfigDir, "app.toml")
	}
	if i.SandboxRoot == "" {
		i.SandboxRoot = config.Default().Sandbox.Root
	}
	defaults := config.Default().FileDelivery
	if i.FileDelivery.MaxDirectBase64Bytes <= 0 {
		i.FileDelivery.MaxDirectBase64Bytes = defaults.MaxDirectBase64Bytes
	}
	if i.FileDelivery.Backend == "" {
		i.FileDelivery.Backend = defaults.Backend
	}
	if i.FileDelivery.S3Region == "" {
		i.FileDelivery.S3Region = defaults.S3Region
	}
	if i.Now == nil {
		i.Now = time.Now
	}
	return i
}

func (i Info) CurrentTime() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

func AtriyphRuleCard() string {
	return atriyph.RuleCard()
}
