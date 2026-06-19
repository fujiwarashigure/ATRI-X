package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
	"github.com/fujiwarashigure/ATRI-X/internal/utils/fileops"
)

const FinalizeAtrixSkillName = "finalize_atrix_skill"

type FinalizeAtrixSkillTool struct {
	Manager *Manager
}

type finalizeAtrixSkillArgs struct {
	Name      string `json:"name"`
	TimeoutMS int    `json:"timeout_ms"`
}

func NewFinalizeAtrixSkillTool(manager *Manager) FinalizeAtrixSkillTool {
	return FinalizeAtrixSkillTool{Manager: manager}
}

func (FinalizeAtrixSkillTool) Name() string { return FinalizeAtrixSkillName }

func (FinalizeAtrixSkillTool) Info() tool.Info {
	return tool.NewBuilder(FinalizeAtrixSkillName).
		Description("完成 ATRI-X Skill 修改：校验 SKILL.atriyph；如有 main.go，则 gofmt、校验 package main、go build；成功后 reload。错误会返回给 LLM 继续修。").
		Source(tool.SourceBuiltin).
		Risk(tool.RiskHigh).
		BuildInfo()
}

func (FinalizeAtrixSkillTool) Schema() llm.ToolSchema {
	return tool.NewBuilder(FinalizeAtrixSkillName).
		Description("完成 ATRI-X Skill 修改并检查结果。会校验 SKILL.atriyph；如果存在 main.go，会自动 gofmt 写回、校验 package main、go build；成功后 reload。gofmt/build/语法错误会作为结果内容返回，供继续修改。").
		String("name", "skill 名称。", tool.Required()).
		Integer("timeout_ms", "可选，Go 编译超时时间，默认 60000。").
		BuildSchema()
}

func (t FinalizeAtrixSkillTool) Call(ctx context.Context, req tool.CallRequest) (*tool.Result, error) {
	var args finalizeAtrixSkillArgs
	if len(req.Arguments) > 0 {
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse finalize_atrix_skill arguments: %w", err)
		}
	}
	if t.Manager == nil || t.Manager.Registry == nil {
		return nil, fmt.Errorf("skill manager is not configured")
	}
	name := strings.TrimSpace(args.Name)
	if err := validateSkillName(name); err != nil {
		return nil, err
	}
	root := filepath.Join(t.Manager.Root, "go", name)
	atriyphPath := filepath.Join(root, atriyph.SkillFileName)
	atriyphData, err := os.ReadFile(atriyphPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", atriyph.SkillFileName, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "finalize ATRI-X skill %s\n\n", name)
	if _, err := atriyph.ParseSkill(string(atriyphData), name); err != nil {
		fmt.Fprintf(&b, "Atriyph: failed\n%s\n", err)
		return &tool.Result{Content: b.String()}, nil
	}
	b.WriteString("Atriyph: ok\n")

	sourcePath := filepath.Join(root, "main.go")
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		b.WriteString("Go source: not found, text-only skill\n")
		if err := t.Manager.Reload(ctx); err != nil {
			return nil, fmt.Errorf("ATRI-X skill finalized but reload failed: %w", err)
		}
		b.WriteString("reload: ok\n")
		return &tool.Result{Content: b.String()}, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat main.go: %w", err)
	}
	file, err := fileops.ReadFile(sourcePath, "")
	if err != nil {
		return nil, err
	}
	b.WriteString("Go source: found\n")

	formatted, err := formatGoSource(file.Text)
	if err != nil {
		fmt.Fprintf(&b, "gofmt: failed\n%s\n", err)
		return &tool.Result{Content: b.String()}, nil
	}
	if formatted != file.Text {
		if _, err := fileops.WriteTextFile(sourcePath, file, formatted); err != nil {
			return nil, err
		}
		b.WriteString("gofmt: changed\n")
	} else {
		b.WriteString("gofmt: unchanged\n")
	}
	if err := validateGoMainSource(formatted); err != nil {
		fmt.Fprintf(&b, "Go source validation: failed\n%s\n", err)
		return &tool.Result{Content: b.String()}, nil
	}
	b.WriteString("Go source validation: ok\n")

	build := goBuildResult{}
	build, err = runGoBuild(ctx, root, name, args.TimeoutMS)
	if err != nil {
		return nil, err
	}
	if build.Err != nil {
		fmt.Fprintf(&b, "go build: failed\n%v\n", build.Err)
		appendCommandOutput(&b, build.Stdout, build.Stderr)
		return &tool.Result{Content: b.String()}, nil
	}
	b.WriteString("go build: ok\n")
	appendCommandOutput(&b, build.Stdout, build.Stderr)

	if err := t.Manager.Reload(ctx); err != nil {
		return nil, fmt.Errorf("ATRI-X skill finalized but reload failed: %w", err)
	}
	b.WriteString("reload: ok\n")
	return &tool.Result{Content: b.String()}, nil
}

func appendCommandOutput(b *strings.Builder, stdout, stderr string) {
	if strings.TrimSpace(stdout) != "" {
		fmt.Fprintf(b, "stdout:\n%s\n", truncateOutput(stdout))
	}
	if strings.TrimSpace(stderr) != "" {
		fmt.Fprintf(b, "stderr:\n%s\n", truncateOutput(stderr))
	}
}
