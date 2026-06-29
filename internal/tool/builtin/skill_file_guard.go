package builtin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
)

const (
	warnUseReadFile      = "读取本地文件请优先使用 read_file 工具。"
	warnUseEditFile      = "编辑本地文件请优先使用 edit_file 工具。"
	warnUseReadAtrixSkill   = "读取 ATRI-X Skill 文件请使用 read_atrix_skill。"
	warnUseModifyAtrixSkill = "修改 ATRI-X Skill 文件请使用 modify_atrix_skill。"
)

type atrixSkillFile struct {
	Name   string
	Target string
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func detectAtrixSkillFile(skillRoot, path string) (atrixSkillFile, bool) {
	skillRoot = strings.TrimSpace(skillRoot)
	path = strings.TrimSpace(path)
	if skillRoot == "" || path == "" {
		return atrixSkillFile{}, false
	}
	root, err := filepath.Abs(skillRoot)
	if err != nil {
		return atrixSkillFile{}, false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return atrixSkillFile{}, false
	}
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return atrixSkillFile{}, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[0] != "go" || strings.TrimSpace(parts[1]) == "" {
		return atrixSkillFile{}, false
	}
	switch parts[2] {
	case atriyph.SkillFileName:
		return atrixSkillFile{Name: parts[1], Target: "skill_atriyph"}, true
	case "main.go":
		return atrixSkillFile{Name: parts[1], Target: "code_source"}, true
	default:
		return atrixSkillFile{}, false
	}
}

func rejectAtrixSkillFileEdit(skillRoot, path string) error {
	file, ok := detectAtrixSkillFile(skillRoot, path)
	if !ok {
		return nil
	}
	return fmt.Errorf("ATRI-X Skill %s target %s must be modified with modify_atrix_skill", file.Name, file.Target)
}

func readFileWarnings(skillRoot, path string) []string {
	if _, ok := detectAtrixSkillFile(skillRoot, path); ok {
		return []string{warnUseReadAtrixSkill}
	}
	return nil
}
