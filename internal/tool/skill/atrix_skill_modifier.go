package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
	"github.com/fujiwarashigure/ATRI-X/internal/utils/fileops"
)

const (
	ReadAtrixSkillName   = "read_atrix_skill"
	ModifyAtrixSkillName = "modify_atrix_skill"

	atrixSkillTargetAtriyph      = "skill_atriyph"
	atrixSkillTargetCodeSource = "code_source"
)

type ReadAtrixSkillTool struct {
	Manager *Manager
}

type ModifyAtrixSkillTool struct {
	Manager *Manager
}

type readAtrixSkillArgs struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type modifyAtrixSkillArgs struct {
	Name    string            `json:"name"`
	Target  string            `json:"target"`
	Content string            `json:"content"`
	Patches []modifyLinePatch `json:"patches"`
}

type modifyLinePatch struct {
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	NewLines  []string `json:"new_lines"`
}

func NewReadAtrixSkillTool(manager *Manager) ReadAtrixSkillTool {
	return ReadAtrixSkillTool{Manager: manager}
}

func NewModifyAtrixSkillTool(manager *Manager) ModifyAtrixSkillTool {
	return ModifyAtrixSkillTool{Manager: manager}
}

func (ReadAtrixSkillTool) Name() string { return ReadAtrixSkillName }

func (ReadAtrixSkillTool) Info() tool.Info {
	return tool.NewBuilder(ReadAtrixSkillName).
		Description("按行读取 ATRI-X 原生 ATRI-X Skill 的 SKILL.atriyph 或 main.go，返回 1-based 行号，供修改前定位使用。").
		DependsOn("modify_atrix_skill").
		Source(tool.SourceBuiltin).
		Risk(tool.RiskMedium).
		BuildInfo()
}

func (ReadAtrixSkillTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{
		Name:        ReadAtrixSkillName,
		Description: "按行读取 ATRI-X 原生 ATRI-X Skill 文件。target 可选：skill_atriyph 读取 SKILL.atriyph；code_source 读取 main.go。默认读取 SKILL.atriyph。start_line/end_line 为 1-based，可省略以读取全文。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":       map[string]any{"type": "string", "description": "skill 名称。"},
				"target":     map[string]any{"type": "string", "enum": []string{atrixSkillTargetAtriyph, atrixSkillTargetCodeSource}, "description": "读取目标：skill_atriyph 表示 SKILL.atriyph；code_source 表示 main.go。默认 skill_atriyph。"},
				"start_line": map[string]any{"type": "integer", "description": "可选，1-based 起始行，默认 1。"},
				"end_line":   map[string]any{"type": "integer", "description": "可选，1-based 结束行，默认文件末尾。"},
			},
			"required": []string{"name"},
		},
	}}
}

func (t ReadAtrixSkillTool) Call(ctx context.Context, req tool.CallRequest) (*tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args readAtrixSkillArgs
	if len(req.Arguments) > 0 {
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse read_atrix_skill arguments: %w", err)
		}
	}
	path, _, err := atrixSkillTargetPath(t.Manager, strings.TrimSpace(args.Name), strings.TrimSpace(args.Target))
	if err != nil {
		return nil, err
	}
	file, err := fileops.ReadFile(path, "")
	if err != nil {
		return nil, err
	}
	lines := fileops.SplitLines(file.Text)
	start := args.StartLine
	if start <= 0 {
		start = 1
	}
	end := args.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end+1 || start > len(lines)+1 {
		return nil, fmt.Errorf("start_line %d is out of range", args.StartLine)
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, lines[i-1])
	}
	return &tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

func (ModifyAtrixSkillTool) Name() string { return ModifyAtrixSkillName }

func (ModifyAtrixSkillTool) Info() tool.Info {
	return tool.NewBuilder(ModifyAtrixSkillName).
		Description("修改 ATRI-X 原生 ATRI-X Skill 的 SKILL.atriyph 或 main.go；技能定义会校验 Atriyph 并 reload，源码只写入文件，完成后用 finalize_atrix_skill 格式化和编译。").
		DependsOn(FinalizeAtrixSkillName, "read_atrix_skill").
		Source(tool.SourceBuiltin).
		Risk(tool.RiskHigh).
		BuildInfo()
}

func (ModifyAtrixSkillTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{
		Name:        ModifyAtrixSkillName,
		Description: "修改 ATRI-X 原生 ATRI-X Skill 文件。target 可选：skill_atriyph 修改 SKILL.atriyph；code_source 修改 main.go；默认 skill_atriyph。content 用于全量写入；patches 用原文件 1-based 行号替换整行范围。content 与 patches 必须二选一。code_source 只写源码并返回 diff，不会格式化、编译或 reload；完成后调用 finalize_atrix_skill 检查。skill_atriyph 会校验 Atriyph 并 reload。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "skill 名称。"},
				"target":  map[string]any{"type": "string", "enum": []string{atrixSkillTargetAtriyph, atrixSkillTargetCodeSource}, "description": "修改目标：skill_atriyph 表示 SKILL.atriyph；code_source 表示 main.go。默认 skill_atriyph。"},
				"content": map[string]any{"type": "string", "description": "可选，完整新的文件内容。"},
				"patches": map[string]any{"type": "array", "description": "可选，按原文件行号替换整行范围；new_lines 为空数组表示删除。", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"start_line": map[string]any{"type": "integer", "description": "1-based 起始行，包含。"},
						"end_line":   map[string]any{"type": "integer", "description": "1-based 结束行，包含。"},
						"new_lines":  map[string]any{"type": "array", "description": "替换后的行数组，每个元素不包含换行符。", "items": map[string]any{"type": "string"}},
					},
					"required": []string{"start_line", "end_line", "new_lines"},
				}},
			},
			"required": []string{"name"},
		},
	}}
}

func (t ModifyAtrixSkillTool) Call(ctx context.Context, req tool.CallRequest) (*tool.Result, error) {
	var args modifyAtrixSkillArgs
	if len(req.Arguments) > 0 {
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse modify_atrix_skill arguments: %w", err)
		}
	}
	name := strings.TrimSpace(args.Name)
	path, target, err := atrixSkillTargetPath(t.Manager, name, strings.TrimSpace(args.Target))
	if err != nil {
		return nil, err
	}
	hasContent := args.Content != ""
	hasPatches := len(args.Patches) > 0
	if hasContent == hasPatches {
		return nil, fmt.Errorf("provide exactly one of content or patches")
	}
	file, err := fileops.ReadFile(path, "")
	if err != nil {
		return nil, err
	}
	next := args.Content
	if hasPatches {
		next, err = applyLinePatches(fileops.SplitLines(file.Text), args.Patches)
		if err != nil {
			return nil, err
		}
	}
	switch target {
	case atrixSkillTargetAtriyph:
		return t.modifyAtriyph(ctx, name, path, file, next)
	case atrixSkillTargetCodeSource:
		return t.modifyCodeSource(name, path, file, next)
	default:
		return nil, fmt.Errorf("unsupported target %q", target)
	}
}

func (t ModifyAtrixSkillTool) modifyAtriyph(ctx context.Context, name, path string, file fileops.File, next string) (*tool.Result, error) {
	next = normalizeSkillContent(next)
	if _, err := atriyph.ParseSkill(next, name); err != nil {
		return nil, err
	}
	diff := fileops.UnifiedDiff(path, fileops.SplitLines(fileops.NormalizeEditText(file.Text)), fileops.SplitLines(fileops.NormalizeEditText(next)), 3)
	if _, err := fileops.WriteTextFile(path, file, next); err != nil {
		return nil, err
	}
	if err := t.Manager.Reload(ctx); err != nil {
		return nil, fmt.Errorf("ATRI-X skill modified but reload failed: %w", err)
	}
	return &tool.Result{Content: fmt.Sprintf("modified ATRI-X skill %s target %s\ndiff:\n%s", name, atrixSkillTargetAtriyph, diff)}, nil
}

func (t ModifyAtrixSkillTool) modifyCodeSource(name, path string, file fileops.File, next string) (*tool.Result, error) {
	next = normalizeSkillContent(next)
	diff := fileops.UnifiedDiff(path, fileops.SplitLines(fileops.NormalizeEditText(file.Text)), fileops.SplitLines(fileops.NormalizeEditText(next)), 3)
	if _, err := fileops.WriteTextFile(path, file, next); err != nil {
		return nil, err
	}
	return &tool.Result{Content: fmt.Sprintf("modified ATRI-X skill %s target %s\ndiff:\n%s\n\n源码已写入但尚未格式化/编译；完成修改后调用 finalize_atrix_skill。", name, atrixSkillTargetCodeSource, diff)}, nil
}

func atrixSkillTargetPath(manager *Manager, name, target string) (string, string, error) {
	if manager == nil || manager.Registry == nil {
		return "", "", fmt.Errorf("skill manager is not configured")
	}
	if err := validateSkillName(name); err != nil {
		return "", "", err
	}
	if target == "" {
		target = atrixSkillTargetAtriyph
	}
	filename := ""
	switch target {
	case atrixSkillTargetAtriyph:
		filename = atriyph.SkillFileName
	case atrixSkillTargetCodeSource:
		filename = "main.go"
	default:
		return "", "", fmt.Errorf("invalid target %q; use %q or %q", target, atrixSkillTargetAtriyph, atrixSkillTargetCodeSource)
	}
	path := filepath.Join(manager.Root, "go", name, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", "", fmt.Errorf("ATRI-X skill %s %q not found", target, name)
	} else if err != nil {
		return "", "", fmt.Errorf("stat %s: %w", filename, err)
	}
	return path, target, nil
}

func splitSkillLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

func applyLinePatches(lines []string, patches []modifyLinePatch) (string, error) {
	sorted := append([]modifyLinePatch(nil), patches...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartLine < sorted[j].StartLine })
	previousEnd := 0
	for _, patch := range sorted {
		if patch.StartLine < 1 || patch.EndLine < patch.StartLine || patch.EndLine > len(lines) {
			return "", fmt.Errorf("invalid patch range %d-%d", patch.StartLine, patch.EndLine)
		}
		if patch.StartLine <= previousEnd {
			return "", fmt.Errorf("patch ranges must not overlap")
		}
		for _, line := range patch.NewLines {
			if strings.ContainsAny(line, "\r\n") {
				return "", fmt.Errorf("new_lines must not contain line breaks")
			}
		}
		previousEnd = patch.EndLine
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		patch := sorted[i]
		start := patch.StartLine - 1
		end := patch.EndLine
		next := make([]string, 0, len(lines)-(end-start)+len(patch.NewLines))
		next = append(next, lines[:start]...)
		next = append(next, patch.NewLines...)
		next = append(next, lines[end:]...)
		lines = next
	}
	return strings.Join(lines, "\n"), nil
}

func normalizeSkillContent(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimRight(text, "\n") + "\n"
}
